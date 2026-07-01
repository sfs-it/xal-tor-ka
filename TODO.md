# Xal-Tor-Ka — TODO

## In corso

_(nessuna voce attiva — prossimo passo da decidere: vedi candidati in «Da fare»)_

## Da fare

* [ ] **2026-06-20 —** TLS `selfsigned` autogenerato + `GET /setup/ca.crt` (§3.1)
* [ ] **2026-06-23 —** Alerting: verificare invio reale Telegram/email con credenziali (codice pronto, non testato)
* [ ] **2026-06-29 —** OIDC: provare con credenziali **reali** Google/Microsoft (l'exchange/verifica id_token è già coperto da test mock-IdP; manca la prova live)
* [ ] **2026-06-30 —** Portabilità: **field-test deploy host/LXD** (systemd + `nginx -s reload` + `PROXY_RESOLVER`/`PROXY_UPSTREAM`) — scaffolding pronto, non provato su macchina reale

## Idee / Backlog

* [ ] **2026-06-29 —** Selettore provider nel form crea-utente di `/admin` (oggi gli utenti OIDC si creano da CLI `user --provider <id>`)
* [ ] **2026-06-29 —** OIDC multi-tenant Microsoft (`common`): validazione issuer custom per il placeholder `{tenantid}`

* [ ] **2026-06-20 —** TLS path `acme` (ACME DNS-01 su zona PowerDNS) — rimandato al deploy (BLUEPRINT §18.4)
* [ ] **2026-06-20 —** Wildcard host `*.dominio` nella matrice (BLUEPRINT §5/§18.4)
* [ ] **2026-06-20 —** Promuovere il modulo GO provvisorio di MYRULES a `IA_POLICY/01_DEV/Primitive/0X_GO.md`
* [ ] **2026-06-20 —** Control app mobile come sotto-progetto autonomo (`dev ionic_react`)
* [ ] **2026-06-20 —** File Python di servizio (es. `history_log.py` generatore `HISTORY.md`) — valutare una **repository dedicata**; rimandato

## Fatto

* [x] **2026-06-20 —** `CLAUDE.md` iniziale (init) con architettura e puntatori
* [x] **2026-06-20 —** Profilo IA_POLICY `Progetti/xal-tor-ka.md` + `MYRULES.md` riassemblato a mano (Go+Docker+NGINX+Windows, rimosso React/Ionic)
* [x] **2026-06-20 —** `BLUEPRINT.md` autoritativo (supera il `PROJECT_DETAILS.md` dello zip)
* [x] **2026-06-20 —** Chiuse le decisioni di progetto (BLUEPRINT §18): sessioni RAM+SQLite, secrets separati, users_file, TLS selfsigned (acme parcheggiato), alerting Telegram+email, deploy via env-var
* [x] **2026-06-22 —** Toolchain Go (1.26) installato; build/vet/gofmt puliti
* [x] **2026-06-22 —** Scaffolding Go + flusso `/validate` + provider `local` (matrix, session store RAM, argon2id, TOTP) — compilato e provato end-to-end (login+2FA+listing)
* [x] **2026-06-22 —** Onboarding ibrido: subcommand CLI `setup` (token+email) → wizard web `/setup` (password server-side + enrollment TOTP con QR) → scrive `users.json` + snapshot, ricarica a caldo. Verificato end-to-end.
* [x] **2026-06-22 —** Gestione servizi: `services.json` (backend extra proxati + link esterni) merge nel resolver; `/listing` come griglia di riquadri; CLI `add-backend`/`add-link`; `POST /admin/reload` (IP whitelist) per ricarica a caldo. Verificato.
* [x] **2026-06-22 —** Containerizzazione: Dockerfile multi-stage (distroless nonroot, build statica), `docker-compose.yml` (NGINX esposto, Go interno, volumi, limiti, log-rotation), `nginx/conf.d`, `.dockerignore`, `.env.example`, target Make `up/down/logs/rebuild/ps/setup`. **Build e stack verificati** (curl via NGINX :80 OK).
* [x] **2026-06-22 —** `INSTALL.md`: guida installazione su VPS remota (copia file, `.env`, `make up`, setup ibrido, add servizi, operatività, sicurezza)
* [x] **2026-06-23 —** Proxy manager (`proxy/`): genera `nginx/conf.d/backends.conf` (server{} per host, `auth_request` su route protette, `proxy_pass` upstream) da config+services; NGINX custom con sidecar inotify + `nginx -t` per reload a caldo. **Verificato in Docker**: routing per host, enforcement auth (302 login), public proxato, default_server per il gate, reload a caldo. Test unitari `proxy`.
* [x] **2026-06-23 —** Admin panel `/admin` (IP-whitelist): gestione servizi (`services.json`: backend + link, config.json read-only), utenti (crea con QR TOTP, autorizzazioni, reset 2FA, elimina), persistenza atomica + snapshot + reload. Verificato (add/del/authz/QR, XFF spoof → 403).
* [x] **2026-06-23 —** Health checker (`health/`): probe HTTP periodico per backend (up/down/unreachable), goroutine legata al ctx, sezione Monitoring in `/admin`, alerter Telegram+email su transizione (log sempre). Verificato up/unreachable.
* [x] **2026-06-23 —** UI ridisegnata (Go, no React): design system CSS embeddato (`//go:embed`, servito su `/assets/admin.css`), `/admin` a card+badge+form ordinati, e login/2FA/setup/listing coerenti (auth-card, topbar). Verificato (CSS 200, classi presenti).
* [x] **2026-06-23 —** Backend demo reale nello stack: servizio `demo` (nginx + pagina) interno, `demo.localhost` proxato dal gateway (config.json upstream `http://demo:80`, health reale). Proxy manager reso robusto al cold-start (`resolver` 127.0.0.11 + `proxy_pass` via variabile). Verificato: demo.localhost 200 + Monitoring `up`.
* [x] **2026-06-23 —** `demo` spostato da `config.json` a `services.json` → ora eliminabile/gestibile dall'admin come gli altri. Link cliccabile sulla voce host (card + Monitoring).
* [x] **2026-06-23 —** Scoperta container Docker: sidecar `docker-socket-proxy` read-only (CONTAINERS=1), pacchetto `dockerscan/`, sezione "Scopri container" in `/admin` che propone `<nome>.localhost → host.docker.internal:<porta>` con bottone aggiungi (`POST /admin/discover/add`); `extra_hosts: host-gateway`. **Verificato** (scoperta container reali, add→proxy a Qdrant 200, delete).
* [x] **2026-06-23 —** Fix reload NGINX: polling (hash conf.d) invece di inotify (inaffidabile sui bind mount Docker Desktop/WSL2). Add/del servizi si applicano a caldo in ~2s, niente restart. Verificato.
* [x] **2026-06-23 —** Servizi: campo `description` + `disabled` nel modello; card con **edit** (nome/host/url/path/regola/upstream/descrizione), **disabilita/abilita** (escluso da resolver/proxy/health) ed **elimina**; toggle disabilita anche per i link. Verificato (edit, disable→fuori proxy, enable→torna).
* [x] **2026-06-23 —** Scan porte host: `dockerscan.ScanPorts` (TCP connect su host.docker.internal, range limitato), sezione "Porte host" + pagina `/admin/hostscan` con add → vhost `<nome>.localhost`. Caso d'uso tunnel PuTTY/SSH. Verificato (trova porte host, add→proxy 200).
* [x] **2026-06-23 —** Login admin con password (`handlers/adminauth.go`): `/admin` ora richiede IP whitelist + sessione password (hash in `secrets.json`). Primo accesso default `admin@localhost`/`password` mostrato sulla login, con **cambio obbligatorio**. Cambio scrive `secrets.json` (atomico+snapshot). Verificato end-to-end (redirect→login→must-change→accesso, vecchia pw 401).
* [x] **2026-06-23 —** Admin: scan porte host con **checkbox + «Aggiungi selezionati»** (bulk, nome auto `host-<porta>`); **modifica email utente**; **normalizzazione upstream** `localhost`/`127.0.0.1` → `host.docker.internal` in add/edit. Verificato.
* [x] **2026-06-23 —** Fix accesso backend `authenticated`/`whitelist`: **auth per-host** — ogni vhost serve `/login`,`/auth/`,`/logout`,`/listing`,`/assets/` dal gate e `@login` reindirizza allo STESSO host → cookie **host-only** (affidabile su Chrome/*.localhost; il `Domain=localhost` non lo è). `session.cookie_domain` configurabile (default vuoto; dominio padre reale per SSO in prod). **reset password utente** dal pannello (`/admin/user/password`). Verificato (login/assets su demo.localhost, backends.conf per-host).
* [x] **2026-06-23 —** Sessioni persistenti: store `file` (write-behind su `data/sessions.json` solo su create/2FA/delete, non sul path di richiesta), reload all'avvio → login sopravvive al restart. Test unitari.
* [x] **2026-06-23 —** Backup auto-trash (mantiene ultimi 10 per tipo) su users/services/secrets/backends.conf; CLI `backups` (lista) e `restore --snapshot=<n>` + `make reset-admin`/`set-admin-pw`/`reset-admin-pw`. Test prune+restore.
* [x] **2026-06-23 —** Matrix: match path per-segmento (`/api` non matcha `/apixyz`). Test.
* [x] **2026-06-23 —** Test automatici: `config.Validate`, password argon2id, TOTP, matrix, sessioni persistenti, prune/restore. `go test -race ./...` verde.
* [x] **2026-06-24 —** Admin in pagine separate per URL (/admin riepilogo, /servizi, /docker, /utenti, /monitoring) con topbar condivisa e dati on-demand (scan Docker solo su /admin/docker); voce attiva, ritorno a pagina di provenienza dopo le mutazioni. Verificato (tutte 200).
* [x] **2026-06-24 —** Auth unificata: admin = utente con flag `admin` (niente password admin separata in secrets.json). `/admin` = IP whitelist + sessione utente + flag admin; rimossi login/sessione admin dedicati. Setup crea il primo admin; toggle admin in /admin/utenti (almeno 1 admin); CLI `xaltorka user --email --password --admin` (+ `make admin`) per bootstrap/recovery. Migrato admin@localhost a admin. Verificato.
* [x] **2026-06-24 —** Admin accede ovunque (bypass whitelist in /validate e dashboard). UI utenti: tabella pulita con popup host (<details>) + **pagina proprietà** `/admin/utenti/<email>` (email, admin, password, 2FA, autorizzazioni). fail2ban: log `logs/auth.log` dei fallimenti (login/2FA/admin) con IP reale + filtro/jail di esempio. Storage: confermato JSON (no DB ora). Verificato.
* [x] **2026-06-29 —** Provider OIDC (Google, Microsoft/Entra, generici Keycloak/Authentik/Auth0/Okta/GitLab) via `coreos/go-oidc` + `x/oauth2`: `providers/oidc.go` (discovery lazy, AuthURL, Exchange con verifica id_token+nonce, fail-closed), handler `/auth/{provider}/start|callback` (state/nonce in cookie `xtk_oidc`, anti-CSRF), bottoni "Accedi con…" nel `/login`, no auto-provisioning (utente deve esistere con `provider=<id>`), CLI `user --provider`, esempi disabilitati in `config.json`/`secrets.example.json`, validazione (oidc abilitato richiede issuer+client_id), guida `AUTH-PROVIDERS.md` + `INSTALL.md` §5-quater. **Verificato** discovery+redirect reale su Google (303→accounts.google.com, cookie stato); callback/exchange resta da provare con credenziali reali.
* [x] **2026-06-30 —** Versioning `beta0.1`: fonte unica `version/version.go` (override via ldflags), esposta in CLI `version`/`-v`, `/healthz`, log d'avvio, topbar admin, footer login, `LABEL` OCI Docker, target `make build`/`make version`. Test.
* [x] **2026-06-30 —** Test unitari del nuovo codice: mock-IdP OIDC (Exchange/firma id_token/nonce/fallback), handler OIDC (state cookie, buttons), `buildOIDC`, validazione oidc, reload proxy, `hostInternalize`, version. `go test -race` verde.
* [x] **2026-06-30 —** Pubblicazione: git init + branch `main`/`beta0.1`, `README.md`, **LICENSE Apache-2.0** + `NOTICE` (SFS.it di Zanutto Agostino), `services.json` e materiale IA gitignorati (backup a parte). Commit senza Claude-Session/Co-Authored-By (credito in prosa).
* [x] **2026-06-30 —** Portabilità oltre Docker (knob env, default Docker invariati): `DEPLOY_MODE`, `NGINX_RELOAD_CMD` (hook reload in `proxy.Manager`), `UPSTREAM_LOCALHOST` (`hostInternalize` configurabile); unit `deploy/xaltorka.service` + `deploy/xaltorka.sudoers`, `INSTALL.md` §9. Docker verificato invariato; host/LXD da provare sul campo.
* [x] **2026-06-30 —** Documentazione: inglese ufficiale in root (`README` per decisore + 3° paragrafo → `TECHNOTES`, `TECHNOTES`, `REQUIREMENTS`; `INSTALL`/`AUTH-PROVIDERS` tradotti in EN). Traduzioni entry (README+TECHNOTES) in 9 lingue sotto `DOCS/` (it, fr, es, de, ru, pt, zh, hi, ar) + indice `DOCS/README.md` e language switcher. Generate via subagent in parallelo.
* [x] **2026-07-01 —** Fase 1 IP allow-list: campo `ip_allow` (CIDR) per backend con enforce fail-closed in `/validate` prima della regola (vale anche per `public`), edit/visualizza in `/admin/servizi`; whitelist IP admin globale gestibile in UI (override in `services.json`, hot reload, guard anti-lockout) via `/admin/adminips`. Validazione CIDR + test. Build/vet/race verdi.
* [x] **2026-07-01 —** Fase 1b client-IP plumbing/hardening: `trusted_proxies` ora include `${EDGE_CIDR}` (+127.0.0.1/::1); `clientIP` preferisce `X-Real-IP` (settato dall'edge a `$remote_addr`, non spoofabile) con fallback XFF; auth location NGINX (base+generato) passa `X-Real-IP`. Ora i controlli IP (admin + `ip_allow`) vedono il **client reale**. Verificato anti-lockout: /admin via NGINX → 303 (non 403), log mostra IP client reale 172.21.0.1 ∈ ADMIN_CIDR. Test `clientIP` (prefer X-Real-IP + spoof-proof da peer non fidato).
* [x] **2026-07-01 —** Fase 2 `/listing`: link «Amministrazione» per admin + bottone «Profilo» per tutti → pagina self-service `/profilo` (email/provider/ruolo, servizi accessibili, **cambio password** con verifica di quella attuale per account locali, **rigenera 2FA** con nuovo QR). Rotte `GET /profilo`, `POST /profilo/password|totp`. Build/vet/test verdi; endpoint verificati (303→login senza sessione).
