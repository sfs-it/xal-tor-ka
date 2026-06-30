# BLUEPRINT — Xal-Tor-Ka

> Blueprint architetturale **autoritativo e rigoroso**, scritto per permettere di
> rigenerare il codice da zero "alla cieca". Supera e precisa il
> `PROJECT_DETAILS.md` contenuto in `PHONETALK=chat=Xal-Tor-Ka.zip` (design-stage,
> schema solo concettuale). In caso di conflitto su dettagli implementativi, **vince
> questo file**; per il *perché* delle decisioni resta valido `PROJECT_HISTORY.md`.
>
> Stato: **progettazione** (nessun codice). Convenzioni: testo IT, identificatori/
> codice EN. Placeholder template tra `{...}`. Principio guida trasversale:
> **fail-closed** (nel dubbio si nega).

---

## 1. Scopo e modello di sicurezza

Gatekeeper di autenticazione + manager di reverse proxy in Docker davanti a una VPS
pubblica. Un solo punto esposto: **NGINX** (scudo statico, nessun runtime dinamico
pubblico). NGINX, per ogni richiesta, interroga via `auth_request` il servizio **Go**
(`xaltorka`), **mai esposto direttamente**, che decide l'esito sulla base di una
matrice di autorizzazione per backend. I servizi reali stanno su rete interna e sono
raggiungibili solo attraverso il reverse proxy autenticato.

Invarianti di sicurezza:
- **Default-deny**: nessun match nella matrice ⇒ negato. Errore/timeout
  nell'auth service ⇒ negato (mai `200` per fallback).
- Servizio Go raggiungibile solo dalla rete interna Docker (nessuna porta pubblica).
- `/admin` e `/listing` blindati (IP whitelist e/o utente abilitato).
- Segreti mai nei log, mai in query string; confronti di token in tempo costante.

---

## 2. Architettura dei componenti

```
Internet
   │ HTTPS
   ▼
┌──────────────┐   auth_request (rete interna)   ┌────────────────────────────┐
│   NGINX      │ ──────────────────────────────► │  xaltorka (Go)             │
│ (gatekeeper) │ ◄────────────────────────────── │  - /validate (auth_request)│
│  edge TLS    │   200 pass / 401 login / 403 deny│  - login + provider OIDC   │
└──────────────┘                                  │  - 2FA TOTP / sessioni     │
   │ proxy_pass (solo se 200)                      │  - matrice utente→backend  │
   ▼                                               │  - /admin /listing         │
┌────────────────────────────┐                     │  - health checker          │
│ backend interni (upstream) │                     │  - backup/restore          │
│ su rete interna / VPS      │                     └────────────────────────────┘
└────────────────────────────┘                          ▲ legge/scrive
                                                          │
                                              config.json + backups/ (volumi)
```

---

## 3. Topologia Docker

Servizi nel `docker-compose.yml`:

| Servizio   | Immagine                | Rete                | Porte                          | Volumi |
|------------|-------------------------|---------------------|--------------------------------|--------|
| `nginx`    | nginx:stable-alpine     | `edge` + `internal` | `80` (dietro terminatore TLS esterno) | conf vhost (ro) |
| `xaltorka` | build locale (Go statico) | `internal`        | **nessuna porta pubblicata**   | `config.json`, `secrets.json`, `users.json`, `backups/`, `data/` (SQLite) |

- Rete `internal`: `internal: true` quando il container Go non deve uscire (uscirà
  però verso i provider OIDC e gli upstream → vedi Decisioni aperte §18).
- Il servizio Go è raggiungibile da NGINX come `http://xaltorka:{GO_PORT}`.
- Limiti risorse e log-rotation obbligatori su ogni servizio (vedi MYRULES Docker §3).
- **TLS terminato esternamente** (§18.5): NGINX riceve in chiaro dal terminatore.
  Configura `set_real_ip_from {EDGE_CIDR}` + `real_ip_header X-Forwarded-For`, fidati
  di `X-Forwarded-Proto` solo dai `trusted_proxies` e propagalo al servizio Go.

### 3.1 TLS edge & provisioning certificati (§18.4 / §18.5)

In `auth_mode=true` (full-proxy + auth) un **certificato TLS valido è obbligatorio**
all'edge: i cookie di sessione `Secure` e l'OIDC richiedono HTTPS. Catena di
provisioning, per priorità, selezionata da `config.json` → `tls.mode`:

1. **`external`** (default produzione, §18.5): un reverse proxy/LB a monte termina il
   TLS; lo stack riceve in chiaro dietro `trusted_proxies`. NGINX dello stack non
   gestisce certificati.
2. **`acme`** (preferito quando lo stack auto-termina): emissione/rinnovo automatico
   di un certificato — anche **wildcard** `*.dominio` (coerente col §18.4 "sì al
   wildcard") — da CA pubblica (Let's Encrypt) via challenge **DNS-01 contro una zona
   PowerDNS** tramite API. Credenziali API in `secrets.json`. **⏸ RIMANDATO** alla
   fase di deploy: per ora uso locale, non implementare (vedi §18.4).
3. **`selfsigned`** (fallback garantito: dev/localhost o senza DNS pubblico/CA): la
   Docker genera all'avvio una CA + cert self-signed e li **espone per il download**
   (`GET /setup/ca.crt`, ristretto), così l'utente li **installa nel trust store** del
   proprio computer ed evita gli avvisi del browser.

---

## 4. Contratto `auth_request` (cuore del gatekeeper)

### 4.1 Lato NGINX
```nginx
# subrequest interna verso il servizio Go
location = /__auth {
    internal;
    proxy_pass              http://xaltorka:{GO_PORT}/validate;
    proxy_pass_request_body off;
    proxy_set_header        Content-Length "";
    proxy_set_header        X-Original-Host $host;
    proxy_set_header        X-Original-URI  $request_uri;
    proxy_set_header        X-Original-IP   $remote_addr;
    # il cookie di sessione viaggia di default con la subrequest
}

location / {
    auth_request          /__auth;
    auth_request_set      $auth_user $upstream_http_x_auth_user;
    error_page 401 = @login;
    error_page 403 = @forbidden;
    proxy_set_header      X-Auth-User $auth_user;   # identità verso l'upstream
    proxy_pass            http://{UPSTREAM};        # risolto da config (vedi nota)
}
location @login     { return 302 {EXTERNAL_URL}/login?next=$request_uri; }
location @forbidden { return 403; }
```
> Nota: la mappatura `host+path → upstream` è generata dal manager di proxy a partire
> da `config.json` (§16.3); NGINX non conosce le regole, solo gli upstream.

### 4.2 Lato Go — endpoint `GET /validate`
Input (header): `X-Original-Host`, `X-Original-URI`, `X-Original-IP`, `Cookie`.
Algoritmo (fail-closed):
1. Risolvi `(host, path)` nella matrice (§5). Nessun match ⇒ **403**.
2. Regola della route:
   - `public` ⇒ **200**.
   - `authenticated` ⇒ sessione valida **con 2FA completato** ⇒ 200; altrimenti **401**.
   - `whitelist` ⇒ sessione valida **e** `user ∈ backend.allowed` ⇒ 200; sessione
     valida ma non autorizzato ⇒ **403**; nessuna sessione ⇒ **401**.
3. Su 200, opzionale header di risposta `X-Auth-User: {email}` inoltrato all'upstream.

Semantica per NGINX: **200** = pass · **401** = redirect login · **403** = negato.
Qualunque errore interno ⇒ 403 (default-deny), loggato.

---

## 5. Matrice di autorizzazione — algoritmo di risoluzione

Risoluzione deterministica di `(host, path)`:
1. **Host**: match esatto su `backend.host`; poi eventuale wildcard `*.dominio`
   (vedi Decisioni aperte §18). Nessun host ⇒ 403.
2. **Route**: tra le `routes` del backend, scegli quella col **prefisso di path più
   lungo** che è prefisso di `path` (longest-prefix match). `/` fa da catch-all.
3. **Regola**: applica `rule` della route scelta (`public` | `authenticated` | `whitelist`).
4. **Whitelist**: l'autorizzazione utente→backend è a livello di `backend.id`
   (l'utente abilita l'intero backend; la granularità per-route nasce dalle `rule`).

Esempio (da PROJECT_DETAILS, ora formalizzato):
- `segnalapia.it/` → `public`
- `segnalapia.it/api` → `authenticated`
- `segnalapia.it/frontend` → `whitelist` (solo utenti con `"segnalapia"` in `backends`)
- `pippo@…` abilitato a `["segnalapia","processionaria"]`; `mario@…` a `["segnalapia"]`.

---

## 6. Provider di autenticazione

Interfaccia comune Go, implementazioni intercambiabili attivabili da config:
```go
type Provider interface {
    ID() string
    Type() string // "oidc" | "local"
    // OIDC: URL a cui redirigere l'utente; Local: non usato
    AuthURL(ctx context.Context, state, nonce string) (string, error)
    // Scambia il callback per l'identità verificata (email)
    Exchange(ctx context.Context, r *http.Request) (Identity, error)
}
type Identity struct { Email string; Provider string }
```
- **`oidc`** (Google, Microsoft): OAuth2/OIDC standard, `issuer`/`client_id`/
  `client_secret` da config; verifica `state`+`nonce`.
- **`local`**: credenziali in store locale (file/DB nel volume); password con
  **argon2id**.
- **2FA TOTP** (package `auth/`): segreto per-utente (Base32), finestra ±1 step,
  obbligatorio per le regole `authenticated`/`whitelist`. Già usato in passato con
  Google Authenticator.

Flusso login: `/login` (scelta provider) → `/auth/{id}/start` → provider →
`/auth/{id}/callback` (crea sessione *pre-2FA*) → prompt TOTP → `POST /auth/totp`
(promuove la sessione a *2FA-ok*).
- **Enrollment TOTP**: alla creazione utente il `totp_secret` (Base32) è generato dal
  sistema e fornito come URI `otpauth://totp/...` + QR. (Vedi §18.4: "scaricabile per
  i client" da confermare.)

---

## 7. Configurazione su file — split logica / segreti / utenti

**Tre** file montati come volume (decisione §18.2: segreti separati), decodificati
con `DisallowUnknownFields` (campo sconosciuto ⇒ Fail-Fast all'avvio col path del
campo). Interpolazione `${VAR}` / `${VAR:-default}` risolta al load: i valori
specifici di deploy arrivano da **variabili d'ambiente** (12-factor), con default
`localhost` per il testing (§18.7).

### 7.1 `config.json` — logica, nessun segreto
```json
{
  "auth_mode": true,
  "server": {
    "listen": "${GO_PORT:-:8080}",
    "external_url": "${GATE_URL:-http://localhost:8080}",
    "trusted_proxies": ["${EDGE_CIDR:-127.0.0.1/32}"]
  },
  "tls": {
    "mode": "${TLS_MODE:-selfsigned}",
    "domains": ["{GATE_HOST}", "*.{GATE_HOST}"],
    "acme": { "provider": "powerdns", "pdns_api_url": "${PDNS_API_URL}", "email": "{ACME_EMAIL}" }
  },
  "session": {
    "cookie_name": "xtk_session",
    "ttl_minutes": 720,
    "idle_timeout_minutes": 60,
    "store": "sqlite",
    "sqlite_path": "data/xaltorka.db"
  },
  "admin": { "ip_whitelist": ["${ADMIN_CIDR:-127.0.0.1/32}"] },
  "providers": [
    { "id": "google", "type": "oidc", "enabled": true,
      "issuer": "https://accounts.google.com", "client_id": "${GOOGLE_CLIENT_ID}" },
    { "id": "local", "type": "local", "enabled": true }
  ],
  "users_file": "users.json",
  "secrets_file": "secrets.json",
  "monitoring": {
    "alerting": {
      "telegram": { "enabled": true, "chat_id": "${TG_CHAT_ID}" },
      "email": { "enabled": true, "smtp_host": "${SMTP_HOST}", "from": "alerts@{GATE_HOST}", "to": ["ops@{GATE_HOST}"] }
    }
  },
  "backends": [
    {
      "id": "segnalapia",
      "host": "segnalapia.it",
      "routes": [
        { "path": "/",         "rule": "public",        "upstream": "http://{IP}:80"   },
        { "path": "/api",      "rule": "authenticated", "upstream": "http://{IP}:8000" },
        { "path": "/frontend", "rule": "whitelist",     "upstream": "http://{IP}:3000" }
      ],
      "health": { "url": "http://{IP}:8000/health", "interval_seconds": 30, "timeout_seconds": 5 }
    }
  ]
}
```

### 7.2 `secrets.json` — segreti esterni globali (blindato, perms 600)
```json
{
  "admin_password_hash": "argon2id$v=19$m=65536,t=3,p=4$...",
  "providers": {
    "google":    { "client_secret": "{GOOGLE_CLIENT_SECRET}" },
    "microsoft": { "client_secret": "{MS_CLIENT_SECRET}" }
  },
  "telegram": { "bot_token": "{TG_BOT_TOKEN}" },
  "smtp":     { "username": "{SMTP_USER}", "password": "{SMTP_PASS}" },
  "acme":     { "pdns_api_key": "{PDNS_API_KEY}" }
}
```

### 7.3 `users.json` (`users_file`) — matrice utenti (blindato, perms 600)
```json
{
  "users": [
    { "email": "pippo@segnalapia.it", "provider": "google",
      "totp_secret": "{BASE32}", "backends": ["segnalapia", "processionaria"] },
    { "email": "mario@segnalapia.it", "provider": "local",
      "password_hash": "argon2id$...", "totp_secret": "{BASE32}", "backends": ["segnalapia"] }
  ]
}
```
Contiene i segreti per-utente (`totp_secret`, e `password_hash` per i `local`): file
sensibile, fuori da `config.json` ma — per pragmatismo — non spezzato in
`secrets.json` (ogni utente viaggia con i propri segreti). È la sorgente della cache
utenti in RAM (§8.1).

Vincoli di validazione: `rule ∈ {public,authenticated,whitelist}`; porte upstream
`1..65535`; `session.store ∈ {sqlite,memory}`; ogni `users[].backends[]` riferisce un
`backends[].id` esistente; `providers[].id` univoci; almeno un provider `enabled` se
`auth_mode=true`; utenti `local` richiedono `password_hash`; CIDR validi in
`ip_whitelist`/`trusted_proxies`; `tls.mode ∈ {external,acme,selfsigned}` (con
`acme` richiede `acme.pdns_api_url` + `secrets.acme.pdns_api_key`).

---

## 8. Modelli dati (Go, package `models/`)

```go
type Config struct {
    AuthMode  bool        `json:"auth_mode"`
    Server    ServerCfg   `json:"server"`
    Session   SessionCfg  `json:"session"`
    Admin     AdminCfg    `json:"admin"`
    Providers []ProviderCfg `json:"providers"`
    Backends  []Backend   `json:"backends"`
    Users     []User      `json:"users"`
}
type Backend struct {
    ID     string  `json:"id"`
    Host   string  `json:"host"`
    Routes []Route `json:"routes"`
    Health Health  `json:"health"`
}
type Route struct {
    Path     string `json:"path"`
    Rule     string `json:"rule"`      // public|authenticated|whitelist
    Upstream string `json:"upstream"`
}
type User struct {
    Email      string   `json:"email"`
    Provider   string   `json:"provider"`
    TOTPSecret string   `json:"totp_secret"` // mai loggato
    Backends   []string `json:"backends"`
}
type Session struct {
    ID        string    // opaco, random 256-bit
    Email     string
    Provider  string
    TwoFADone bool
    CreatedAt time.Time
    LastSeen  time.Time
}
type SetupToken struct {
    Value     string    // random base64url, mai loggato in chiaro dopo la stampa iniziale
    ExpiresAt time.Time
    Used      bool
}
```

### 8.1 Store sessioni & cache RAM (decisione §18.1)
Cache RAM in-process **autoritativa**: utenti (read-only, da `users.json`, ricaricati
su modifica/save admin) e sessioni (`map[id]Session` con `RWMutex`). **SQLite**
(`session.sqlite_path` nel volume `data/`) è la sola **durabilità write-behind** delle
sessioni (scrittura su create/destroy + flush periodico di `LastSeen`; load all'avvio).
Hot path `/validate` e match login = puro RAM. Bruteforce → solo RAM (rate-limit +
lookup), niente I/O disco finché un login non riesce. **Nessun daemon di cache esterno.**

---

## 9. Endpoint HTTP (tabella autoritativa)

| Metodo | Path                          | Accesso              | Scopo / Risposta |
|--------|-------------------------------|----------------------|------------------|
| GET    | `/validate`                   | interno (auth_request) | 200/401/403 (§4.2) |
| GET    | `/login`                      | pubblico             | pagina scelta provider |
| GET    | `/auth/{provider}/start`      | pubblico             | 302 → provider OIDC |
| GET    | `/auth/{provider}/callback`   | pubblico             | crea sessione pre-2FA, → prompt TOTP |
| POST   | `/auth/totp`                  | sessione pre-2FA     | verifica TOTP, promuove a 2FA-ok |
| POST   | `/logout`                     | sessione             | invalida sessione |
| GET    | `/listing`                    | sessione + abilitato | dashboard servizi dell'utente |
| GET    | `/admin`                      | IP whitelist (+admin)| pannello |
| GET/POST/PUT/DELETE | `/admin/api/backends[/{id}]` | admin | CRUD backend |
| GET/POST/PUT/DELETE | `/admin/api/users[/{id}]`    | admin | CRUD utenti/matrice |
| POST   | `/admin/api/save`             | admin                | scrive JSON + crea snapshot |
| POST   | `/admin/api/reload`           | admin                | rilegge config e riapplica |
| GET    | `/admin/api/backups`          | admin                | lista snapshot per host |
| POST   | `/admin/api/backups/restore`  | admin                | restore snapshot `{id}` |
| DELETE | `/admin/api/backups/{id}`     | admin                | elimina snapshot (auto-trash protetto) |
| GET    | `/admin/api/monitoring`       | admin                | stato backend + ultimi errori |
| GET    | `/setup`                      | solo finestra setup  | prima configurazione (token) |
| POST   | `/setup`                      | solo finestra setup  | set password admin + primo backend |
| GET    | `/setup/ca.crt`               | ristretto            | download CA self-signed (tls.mode=selfsigned) per trust store client (§3.1) |

I/O JSON rigoroso per le `*/api/*` (tipi, obbligatori/opzionali, status code).
Esempio test: `curl -i -H 'Cookie: xtk_session=...' {EXTERNAL_URL}/validate`.

---

## 10. Admin panel & listing

- **`/admin`** (spartano, servito dal Go): lista/CRUD backend e route; gestione
  matrice utenti→backend; **Save** (scrive `config.json` + snapshot, poi reload);
  sezione **Backups** (lista per host, restore one-click, delete, auto-trash);
  sezione **Monitoring** (stato up/down/unreachable + ultimi errori, alerting opz.).
- **`/listing`**: dashboard dei servizi disponibili per l'utente corrente, con link
  diretti; ristretta a IP/utente abilitato.

---

## 11. Health check (package `health/`)

- Non ICMP: per ogni backend si interroga l'endpoint HTTP `health.url`.
- Stati: `up` (2xx entro `timeout`), `down` (status ≠ 2xx), `unreachable` (errore di
  rete/timeout).
- Periodicità per-backend (`interval_seconds`), `timeout_seconds` per richiesta.
- Memorizza stato corrente + ultimi N errori (timestamp+causa) mostrati in Monitoring.
- Goroutine con `time.NewTicker` legata al context globale (stop su shutdown).
- **Alerting su transizione di stato** (`up ↔ down/unreachable`): **Telegram** (bot) +
  **email** (SMTP), con anti-flapping/debounce. Config in §7.1; `bot_token` e
  credenziali SMTP in `secrets.json` (§7.2).

---

## 12. Backup, snapshot, restore (package `backup/`)

- Ogni `save` da `/admin` crea uno snapshot **prima** di sovrascrivere:
  `backups/{host}/config-{DATETIME}.json` (`DATETIME=YYYYMMDD-HHMM`).
- **Auto-trash**: conserva sempre l'ultimo snapshot valido + ultimi N; elimina i più
  vecchi. **Mai zero snapshot validi.**
- Restore: copia lo snapshot su `config.json`, valida, poi reload. Da `/admin` o CLI.
- CLI di recovery (quando l'interfaccia non è raggiungibile):
  `xaltorka restore --snapshot={id}` (o via `make restore SNAP={id}`).

---

## 13. Setup iniziale (one-time, package `handlers/` + `auth/`)

- Al primo `up` con admin non configurato: genera **SetupToken** (random base64url,
  256-bit), TTL `{SETUP_TTL=15m}`, **uso singolo**, stampato **solo** nei log del
  container.
- `/setup?token=…` valido solo nella finestra: imposta password admin (argon2id) +
  primo backend, poi marca `Used`.
- Scaduto/usato il token, `/setup` risponde 404 e l'interfaccia resta blindata.
- È l'**unico** passaggio che richiede la CLI in condizioni normali.

---

## 14. Modalità `auth_mode`

- `true`: auth + reverse proxy. `/validate` applica la matrice; non autorizzati →
  login/deny.
- `false`: **solo manager di reverse proxy**. `/validate` risponde sempre 200 (passa
  tutto); `/admin` e `/listing` restano attivi per gestire i proxy senza filtro 2FA.

---

## 15. Struttura progetto Go (responsabilità per package)

```
xal-tor-ka/
├── main.go        # entrypoint: load config, server HTTP con timeout, graceful shutdown
├── config/        # load + validate config.json (DisallowUnknownFields), default, errori chiari
├── auth/          # sessioni (store), 2FA TOTP, SetupToken, hashing argon2id
├── providers/     # interfaccia Provider + Local, Google(OIDC), Microsoft(OIDC)
├── handlers/      # /validate, /login, /auth/*, /logout, /listing, /admin(+api), /setup
├── matrix/        # risoluzione (host,path)→rule e check utente→backend (§5)
├── proxy/         # generazione/scrittura config upstream NGINX dal JSON + trigger reload
├── health/        # checker periodico, stati, storico errori
├── backup/        # snapshot per host, auto-trash, restore (+ comando CLI)
├── models/        # Config, Backend, Route, User, Session, SetupToken, ...
├── middleware/    # IP whitelist, logging, rate limiting, recover→500
├── config.json    # volume
├── backups/       # volume
├── Dockerfile · docker-compose.yml · Makefile
```

---

## 16. Build & operatività

### 16.1 Dockerfile (multi-stage, immagine minima)
- Stage build: `golang:alpine`, `go mod download` (cache layer), `CGO_ENABLED=0 go
  build -ldflags="-s -w"`.
- Stage finale: `scratch`/`distroless:nonroot`, copia binario + CA certs, `USER nonroot`.

### 16.2 docker-compose.yml
- Servizi `nginx` + `xaltorka`; reti `edge`/`internal`; volumi `config.json`,
  `backups/`; `restart: unless-stopped`; limiti risorse + log-rotation.

### 16.3 Manager di reverse proxy (package `proxy/`)
- Da `config.json` genera i blocchi upstream/location NGINX; al `save`/`reload`
  riscrive la conf in modo **chirurgico** (no-overwrite, marker), esegue
  `nginx -t`, e su fallimento applica **stop & revert** (vedi MYRULES NGINX §4).

### 16.4 Makefile
- `start` · `stop` · `logs` · `rebuild` · `restore` (documentati inline).

---

## 17. Threat model & failure modes

| Minaccia / guasto                    | Mitigazione |
|--------------------------------------|-------------|
| Auth service down/timeout            | NGINX riceve ≠200 ⇒ deny/login (fail-closed) |
| Bypass diretto del backend           | backend solo su rete interna, nessuna porta pubblica |
| Furto cookie sessione                | `HttpOnly`,`Secure`,`SameSite`; idle+absolute timeout; logout invalida |
| Timing attack su token               | `subtle.ConstantTimeCompare` |
| Brute force login                    | rate limiting (middleware); lockout opzionale |
| Esposizione `/admin`                 | IP whitelist + sessione admin |
| Finestra di setup esposta            | token monouso a scadenza, solo nei log |
| Config corrotta dopo save            | snapshot pre-save + validazione + reload; restore |
| Segreti nei log                      | redazione obbligatoria (token/secret/TOTP) |
| Panic in handler                     | middleware recover ⇒ 500, non abbatte il server |
| Spoofing `X-Forwarded-*` (TLS esterno) | `trusted_proxies`: header di forwarding fidati solo dal terminatore noto |
| Write amplification su disco (bruteforce) | cache RAM autoritativa; SQLite write-behind solo su login riuscito (§8.1) |

---

## 18. Decisioni — stato (chiuse 2026-06-20, salvo residuo §18.4)

1. **Store sessioni** → **SQLite nel volume come durabilità + cache RAM in-process
   autoritativa** (non un daemon separato: il servizio Go è già il daemon; Redis
   aggiungerebbe un container, contro la minimalità). Dettaglio in §8.1.
   - Utenti (poche migliaia) interi in RAM da `users.json`: `/validate` e match login
     = puro RAM, zero letture disco.
   - Sessioni autoritative in RAM; SQLite write-behind (create/destroy + flush
     periodico di `LastSeen`), ricaricate all'avvio.
   - **Bruteforce**: rate-limit + lookup in RAM, i falliti non toccano il disco; solo
     un login riuscito scrive. ✅ confermato.
2. **Segreti** → **file separato** `secrets.json` (§7.2). ✅
3. **Provider local** → **`users_file` dedicato** `users.json` (§7.3). ✅
4. **Wildcard host** → **sì**. Il "autogenerato scaricabile per i client" = **catena
   di provisioning certificati TLS** (§3.1): in full-proxy+auth serve un cert attivo;
   se non si emette un cert reale via **ACME DNS-01 su zona PowerDNS** (anche
   wildcard), la Docker autogenera un **self-signed** scaricabile (`GET /setup/ca.crt`)
   da installare nel trust store del client. ⚠️ *Residuo minore:* confermare
   disponibilità/accesso all'**API PowerDNS** per il path `acme`.
5. **TLS edge** → **terminazione esterna** come default produzione; lo stack sa però
   **auto-terminare** con la catena §3.1 (`external` | `acme` | `selfsigned`). NGINX in
   chiaro dietro il terminatore; `trusted_proxies` + `X-Forwarded-Proto` per cookie
   `Secure` e redirect; HSTS dal terminatore. (§3, §3.1, §17) ✅
6. **Alerting** → **Telegram + email** su transizione di stato backend (§11; config
   §7.1, segreti §7.2). ✅
7. **Valori deploy** → ora **testing su localhost** (default in `config.json`); in
   deploy diventano **variabili d'ambiente** della docker-deployable (§7). ✅

### Parcheggiato (§18.4)
**Uso locale per ora** → `tls.mode=selfsigned` (default). Il path `acme` (ACME DNS-01
su zona PowerDNS) è **rimandato** alla fase di deploy: non implementarlo finché non
serve. Quando si riprenderà servirà: URL API PowerDNS + API key → `secrets.json`.

---

## 19. Cosa aggiunge questo blueprint rispetto a `PROJECT_DETAILS.md`

Schema JSON concreto e tipizzato (§7) · modelli dati Go (§8) · contratto
`auth_request` esatto con semantica status (§4) · algoritmo di risoluzione della
matrice (§5) · tabella endpoint completa (§9) · meccaniche concrete di health (§11),
backup/auto-trash (§12) e setup-token (§13) · topologia Docker e reti (§3) · threat
model (§17) · elenco esplicito delle decisioni aperte (§18).
