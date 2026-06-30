# Xal-Tor-Ka — Installazione su macchina remota

Guida semplice per mettere in produzione il gateway su una VPS (Debian/Ubuntu).
NGINX è l'unico servizio esposto; il servizio Go resta interno.

> **Prerequisiti sulla VPS:** Docker Engine + plugin Compose
> (`docker --version`, `docker compose version`). Su Debian/Ubuntu:
> `curl -fsSL https://get.docker.com | sh`.

---

## 1. Copia i file sulla VPS

Ti serve una cartella (es. `/opt/xaltorka`) con:

```
/opt/xaltorka/
├── docker-compose.yml
├── Dockerfile
├── .env                 # creato da te (vedi punto 2)
├── config.json
├── secrets.json         # creato da te dai .example
├── users.json           # creato da te (vuoto all'inizio)
├── services.json
├── go.mod  go.sum  main.go  *.go  ...   # il sorgente (serve alla build)
└── nginx/conf.d/xaltorka.conf
```

Più semplice: clona/copia l'intero repository nella cartella.

```bash
sudo mkdir -p /opt/xaltorka && sudo chown "$USER":"$USER" /opt/xaltorka
# copia qui i file del progetto (git clone, scp, rsync...)
cd /opt/xaltorka
cp secrets.example.json secrets.json
printf '{ "users": [] }\n' > users.json
```

## 2. Configura l'ambiente (`.env`)

```bash
cp .env.example .env
nano .env
```

Imposta almeno:

| Variabile    | Cosa metterci |
|--------------|---------------|
| `GATE_URL`   | URL pubblico, es. `https://gate.tuodominio.it` |
| `HTTP_PORT`  | porta pubblica di NGINX (default `80`) |
| `TLS_MODE`   | `external` se hai un reverse proxy/LB con TLS davanti; altrimenti `selfsigned` |
| `ADMIN_CIDR` | il TUO IP (o VPN) che potrà usare l'area admin, es. `203.0.113.7/32` |
| `EDGE_CIDR`  | la rete da cui arrivano gli header `X-Forwarded-*` (subnet docker o proxy a monte) |
| `PUID`/`PGID`| output di `id -u` / `id -g` (proprietario dei file di config) |

> **TLS:** in produzione il consiglio è terminare il TLS a monte (reverse proxy
> dell'host o LB) e lasciare `TLS_MODE=external`. La modalità `acme` (Let's Encrypt
> via PowerDNS) è prevista ma non ancora implementata (BLUEPRINT §3.1).

## 3. Avvia lo stack

```bash
make up        # = docker compose up -d --build
make ps        # stato dei servizi
make logs      # log in tempo reale
```

Verifica:

```bash
curl -s http://localhost:${HTTP_PORT:-80}/healthz   # -> {"status":"ok",...}
```

## 4. Crea il primo amministratore (onboarding ibrido)

Il setup parte da CLI e si completa nel browser (niente editing manuale di file).

```bash
make setup EMAIL=admin@tuodominio.it
# oppure:
docker compose run --rm xaltorka setup --email admin@tuodominio.it --config /etc/xaltorka
```

Il comando stampa un URL con un token a scadenza, es.:

```
http://localhost/setup?token=XXXXXXXX
```

Aprilo nel browser (sostituendo l'host con `GATE_URL` se accedi da remoto):

1. la pagina mostra l'email già compilata → imposta la **password**;
2. compare un **QR**: scansionalo con un'app authenticator (Google Authenticator,
   Authy, …) o inserisci la chiave a mano;
3. digita il **codice a 6 cifre** per confermare → profilo attivato.

Da qui in poi: login su `${GATE_URL}/login`.

## 5. Aggiungi servizi

Due tipi di "servizio" compaiono nella dashboard `/listing`:

**a) Backend reverse-proxati** (instradati dal gateway):

```bash
docker compose run --rm xaltorka add-backend \
  --config /etc/xaltorka \
  --id intranet --name "Intranet" \
  --host intranet.tuodominio.it \
  --upstream http://10.0.0.10:8080 \
  --rule whitelist
# applica: riavvia il servizio
make rebuild
```

**b) Link esterni** (solo riquadro/bookmark nella dashboard, non proxati):

```bash
docker compose run --rm xaltorka add-link \
  --config /etc/xaltorka \
  --id wiki --name "Wiki" --url https://wiki.tuodominio.it --public
# applica a caldo (dal TUO IP in ADMIN_CIDR):
curl -X POST http://localhost/admin/reload
```

**c) Scoperta automatica dei container Docker** (comodità, opzionale):
da `/admin` → «Scopri container» vedi i container con porte pubblicate e con un
click crei il vhost `<nome>.localhost → host.docker.internal:<porta>`. La visibilità
passa da un sidecar **read-only** `docker-socket-proxy` (la docker.sock NON è montata
in Xal-Tor-Ka). Per disattivarla: togli il servizio `docker-socket-proxy` dal compose
e la variabile `DOCKER_PROXY`. In produzione Linux serve il supporto a
`host-gateway` (già impostato via `extra_hosts`).

> Autorizzazioni: un servizio `public` (link) o `public`/`authenticated` (backend)
> è visibile a tutti gli utenti loggati. Per i `whitelist` aggiungi l'`id` del
> servizio alla lista `backends` dell'utente in `users.json` (gestione utenti da
> `/admin` in arrivo).

## 5-ter. 2FA (TOTP)

Il secondo fattore TOTP è attivo di default. Per disattivarlo (solo password) imposta
in `config.json` `"disable_totp": true` e riavvia. **In produzione tienilo `false`.**

## 5-quater. Login con provider esterni (Google/Microsoft/OIDC)

Oltre al login locale, puoi abilitare l'accesso via **Google**, **Microsoft (Entra
ID)** o un qualsiasi IdP **OIDC** (Keycloak, Authentik, Auth0, Okta, GitLab). I
provider sono già presenti in `config.json` come esempi **disabilitati**; per
attivarli (registrazione app, `client_id`/`client_secret`, redirect URI, creazione
utenti) segui la guida dedicata **[`AUTH-PROVIDERS.md`](AUTH-PROVIDERS.md)**.

> Dopo aver toccato `config.json` serve **`make rebuild`** (non un semplice
> restart): il binario rilegge lo schema all'avvio e il campo provider è nuovo.

## 5-bis. Amministrazione (modello unificato)

Non esiste una password admin separata: **l'amministratore è un utente**
(`users.json`) con flag `admin: true`. Si accede con il normale login (`/login`);
chi è admin vede anche `/admin` (oltre alla IP whitelist `ADMIN_CIDR`, livello di rete).

- Il **primo admin** è il profilo creato dal setup iniziale (punto 4).
- Creare/promuovere un admin da CLI (recovery/bootstrap):
  `make admin EMAIL=tu@dominio PASSWORD=segreto`
  (equivale a `docker compose run --rm xaltorka user --email … --password … --admin`).
- Da `/admin → Utenti` puoi dare/togliere il flag admin, reimpostare password e 2FA.

## 6. Operatività

| Comando        | Effetto |
|----------------|---------|
| `make up`      | build + avvio in background |
| `make down`    | stop e rimozione container |
| `make logs`    | log in tempo reale |
| `make rebuild` | ricostruzione immagini + riavvio |
| `make ps`      | stato servizi |

**Backup:** ad ogni scrittura di `users.json`/`services.json` viene creato uno
snapshot in `backups/` (timestamp). I segreti (`secrets.json`, `users.json`) non
vanno mai versionati: sono già in `.gitignore`.

## 7. Note di sicurezza

- Il servizio Go **non espone porte**: è raggiungibile solo dalla rete interna docker.
- `secrets.json` e `users.json` contengono segreti → permessi `600`, niente git.
- L'area `/admin` è limitata a `ADMIN_CIDR`. Dietro NGINX, l'IP reale del client
  arriva via `X-Forwarded-For` ed è considerato attendibile solo dalle reti in
  `EDGE_CIDR`.
- In produzione metti il gateway dietro HTTPS (`TLS_MODE=external` + terminazione TLS
  a monte) così i cookie di sessione viaggiano come `Secure`.

## 8. fail2ban (protezione brute-force)

Xal-Tor-Ka scrive i tentativi di accesso falliti (login, 2FA, accesso admin) in
`logs/auth.log`, con l'IP reale del client (via `X-Forwarded-For` dai
`trusted_proxies`). Formato per riga:

```
2026-06-24T10:00:00Z xaltorka auth-fail ip=<IP> event=<login|totp|admin_denied|admin_ip> ...
```

Per attivare il ban sull'host (fail2ban già installato):

```bash
sudo cp fail2ban/filter.d/xaltorka.conf /etc/fail2ban/filter.d/xaltorka.conf
sudo cp fail2ban/jail.d/xaltorka.local /etc/fail2ban/jail.d/xaltorka.local
# adatta logpath in jail.d/xaltorka.local al percorso reale di logs/auth.log
sudo systemctl restart fail2ban
sudo fail2ban-client status xaltorka
```

Il path del log è configurabile con `auth_log` in `config.json` (default `logs/auth.log`).

## 9. Deploy senza Docker (host / LXD / macchina dedicata)

> ⚠️ **Scaffolding beta, non ancora testato end-to-end su una macchina reale.**
> Il *core* (servizio Go) è un binario statico già agnostico al runtime; i punti
> Docker-specifici sono governati da tre **knob** via env (default = Docker):
>
> | Env | Docker (default) | Host/LXD |
> |-----|------------------|----------|
> | `DEPLOY_MODE` | `docker` | `host` |
> | `NGINX_RELOAD_CMD` | *(vuoto: reload a carico del container nginx)* | `nginx -s reload` / `systemctl reload nginx` |
> | `UPSTREAM_LOCALHOST` | `host.docker.internal` | `127.0.0.1` |
>
> `DEPLOY_MODE=host` imposta da solo i default della colonna destra; le altre due
> env lo sovrascrivono se valorizzate.

Passi (Debian/Ubuntu o dentro un container LXD con NGINX installato):

```bash
# 1) compila il binario statico (sulla build machine) e copialo sul target
make build                      # -> ./xaltorka (CGO off, statico)
sudo install -m 0755 xaltorka /usr/local/bin/xaltorka
xaltorka version                # -> beta0.1

# 2) crea utente di servizio e cartella di configurazione
sudo useradd --system --home /opt/xaltorka --shell /usr/sbin/nologin xaltorka
sudo mkdir -p /opt/xaltorka && sudo chown xaltorka:xaltorka /opt/xaltorka
# copia qui config.json, e crea secrets.json/users.json dai .example
sudo -u xaltorka cp secrets.example.json /opt/xaltorka/secrets.json
sudo -u xaltorka sh -c 'printf "{ \"users\": [] }\n" > /opt/xaltorka/users.json'

# 3) NGINX dell'host: includi i vhost generati dal servizio Go
#    (il servizio scrive /opt/xaltorka/nginx/conf.d/backends.conf)
echo 'include /opt/xaltorka/nginx/conf.d/*.conf;' | sudo tee /etc/nginx/conf.d/xaltorka-include.conf

# 4) reload NGINX senza password per l'utente di servizio
sudo install -m 0440 deploy/xaltorka.sudoers /etc/sudoers.d/xaltorka

# 5) servizio systemd
sudo cp deploy/xaltorka.service /etc/systemd/system/xaltorka.service
sudoedit /etc/systemd/system/xaltorka.service   # adatta GATE_URL e percorsi
sudo systemctl daemon-reload
sudo systemctl enable --now xaltorka
journalctl -u xaltorka -f

# 6) onboarding primo admin (come in §4, ma da CLI locale)
sudo -u xaltorka xaltorka setup --email admin@tuodominio.it --config /opt/xaltorka
```

**Punti da verificare sul campo (beta):**
- **Upstream del gate** nei vhost generati: imposta `PROXY_UPSTREAM=127.0.0.1:8080`
  (già nel unit) e fai ascoltare il servizio Go su `127.0.0.1:8080`
  (`server.listen` in `config.json`).
- **`resolver` NGINX**: la generazione usa `PROXY_RESOLVER` (default `127.0.0.11`,
  il DNS interno di Docker). Su host valorizzalo col resolver di sistema
  (es. `127.0.0.53` con systemd-resolved) o adatta i vhost.
- **Permessi reload**: il `sudoers` sopra copre `nginx -s reload`; se usi
  `systemctl reload nginx` aggiorna `NGINX_RELOAD_CMD` e la regola sudoers.
- **TLS**: come in Docker, si assume terminazione a monte (`TLS_MODE=external`).
