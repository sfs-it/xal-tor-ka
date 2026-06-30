# Xal-Tor-Ka — Note tecniche (cosa fa e come)

*Traduzione del documento ufficiale in inglese; in caso di difformità, prevale la versione inglese.*

Una spiegazione tecnica ma leggibile di come funziona. Per la **specifica
rigorosa** (schema JSON, modelli dati, contratto degli endpoint) vedi
[`BLUEPRINT.md`](../../BLUEPRINT.md); per installarlo, vedi [`REQUIREMENTS.md`](../../REQUIREMENTS.md)
+ [`INSTALL.md`](../../INSTALL.md).

## In una frase

Xal-Tor-Ka è un **gatekeeper di autenticazione + manager di reverse proxy**: mette
**NGINX** come unico servizio esposto su internet e, per ogni richiesta, chiede a un
**servizio Go interno** (mai esposto) se far passare, chiedere il login o negare.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## Come decide: il flusso `auth_request`

NGINX **non conosce le regole**. Per ogni richiesta in entrata effettua una
*subrequest* interna all'endpoint `/validate` del servizio Go, passando l'host e il
path originali. Il servizio risponde con uno stato HTTP:

- **200** → NGINX prosegue con `proxy_pass` verso il backend reale;
- **401** → NGINX reindirizza alla pagina di login;
- **403** → accesso negato.

Il principio è **fail-closed**: qualsiasi errore, timeout o dubbio durante la
valutazione produce 401/403, **mai** un 200. Il servizio Go non è raggiungibile
dall'esterno: solo NGINX, sulla rete interna, può interrogarlo.

## Il nucleo: la matrice di autorizzazione

Per ogni **host** e **path**, si applica una di tre regole:

| Regola | Chi entra |
|------|-------------|
| `public` | chiunque |
| `authenticated` | utenti con una sessione valida e **2FA** (TOTP) completata |
| `whitelist` | solo utenti **esplicitamente autorizzati** per quel servizio |

Gli **amministratori** possono accedere a tutto. La granularità è per sottodominio e
per path (es. `/` pubblico ma `/admin` in whitelist sullo stesso host).

## Autenticazione

- **Local**: password con hash **argon2id** + un secondo fattore via **TOTP**
  (RFC 6238, compatibile con Google Authenticator/Authy).
- **OIDC** (OpenID Connect): login delegato a **Google**, **Microsoft/Entra** o
  provider generici (**Keycloak, Authentik, Auth0, Okta, GitLab**). La firma del
  token di identità è verificata contro le chiavi pubbliche del provider.
  - **Nessun auto-provisioning**: l'utente deve già esistere, dichiarato per quel
    provider — accedere con Google non basta per entrare. Vedi
    [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md).
- **Sessioni**: cookie `HttpOnly`/`SameSite=Lax` (e `Secure` dietro HTTPS),
  mantenuti in RAM con persistenza su file (sopravvivono a un riavvio).

## Configurazione: tutto in JSON

Nessun database obbligatorio: la configurazione vive in pochi file JSON tipizzati,
validati all'avvio (**Fail-Fast**: un campo sconosciuto o un valore fuori intervallo
blocca l'avvio con un messaggio chiaro).

| File | Contenuto |
|------|---------|
| `config.json` | infrastruttura (con template da env): modalità di auth, TLS, sessioni, IP admin, provider |
| `secrets.json` | segreti (client secret OIDC, token, SMTP) — mai versionato |
| `users.json` | utenti, ruoli, 2FA, autorizzazioni — mai versionato |
| `services.json` | servizi gestiti a runtime (backend in proxy + link alla dashboard) |

Le modifiche a utenti/servizi si applicano **a caldo** (hot reload), senza riavvio.

## Componenti (servizio Go)

Stdlib-first, binario statico. Package principali:

- `handlers/` — endpoint HTTP: `/validate`, login + TOTP, callback OIDC, setup,
  il pannello `/admin`, la dashboard `/listing`.
- `providers/` — autenticazione: `local` e `oidc` (interfaccia comune).
- `matrix/` — valutazione delle regole di autorizzazione (per host/path).
- `proxy/` — genera i vhost dei backend NGINX e ricarica.
- `health/` — health check periodici dei backend (endpoint `/health`).
- `config/` — load + validazione + salvataggio atomico con snapshot.
- `audit/` — registro dei tentativi di accesso falliti (per fail2ban).
- `auth/` — hashing, TOTP, sessioni, directory utenti.

## Reverse proxy: generazione e reload

Il manager genera la configurazione dei backend NGINX (un `server{}` per host, con
`auth_request` sulle rotte protette e `proxy_pass` verso l'upstream). Il
**reload**:

- **Docker**: il container NGINX rileva la modifica e si ricarica da solo (polling),
  perché `inotify` è inaffidabile sui bind mount di Docker Desktop/WSL2.
- **Host/LXD**: il servizio Go esegue un comando di reload configurabile
  (`nginx -s reload` / `systemctl reload nginx`).

NGINX valida sempre la nuova configurazione e, se non è valida, mantiene quella in
esecuzione: una rigenerazione errata non manda giù il proxy.

## Gestione e operatività

- **Pannello `/admin`** (ristretto per IP): gestisci servizi, utenti e permessi,
  monitora lo stato, in pagine separate.
- **Dashboard `/listing`**: mostra a ogni utente solo i servizi a cui può accedere.
- **Onboarding**: il primo avvio genera un token a scadenza per creare il primo
  amministratore via web; poi l'interfaccia si blinda.
- **Backup**: ogni salvataggio crea uno snapshot con auto-trash (conserva gli ultimi N),
  con restore anche dalla CLI.
- **Difesa dalla forza bruta**: i tentativi falliti finiscono in un log strutturato
  (`logs/auth.log`) con l'IP reale del client, integrabile con **fail2ban**.

## Sicurezza in sintesi

- L'unico servizio esposto è **NGINX**; il servizio Go è interno.
- **Fail-closed** lungo tutto il percorso di autorizzazione.
- Confronti di segreti in **tempo costante**; segreti **mai** loggati.
- Area admin ristretta per **IP**; l'IP reale del client è preso da
  `X-Forwarded-For` solo da proxy fidati.
- Il token di setup è **a uso singolo** e a scadenza.

## Deployment

- **Docker Compose** (predefinito): NGINX esposto, servizio Go interno, un sidecar
  in sola lettura per il discovery dei container, limiti di risorse e rotazione dei
  log.
- **Host / LXD / macchina dedicata**: binario statico + NGINX di sistema, governato da
  tre variabili (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`).
  Vedi [`INSTALL.md`](../../INSTALL.md) §9.

## Versione

Unica fonte di verità in `version/version.go` (linea pre-1.0 `beta0.N`),
sovrascrivibile in fase di build, e mostrata in `xaltorka version`, `/healthz`, il
log di avvio e la UI.
