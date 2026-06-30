# Xal-Tor-Ka — TODO

## In corso

_(nessuna voce attiva — prossimo passo da decidere: vedi candidati in «Da fare»)_

## Da fare

* [ ] **2026-06-20 —** TLS `selfsigned` autogenerato + `GET /setup/ca.crt` (§3.1)
* [ ] **2026-06-23 —** Alerting: verificare invio reale Telegram/email con credenziali (codice pronto, non testato)
* [ ] **2026-06-29 —** OIDC: provare il **round-trip completo** (callback/exchange) con credenziali reali Google/Microsoft — finora verificato solo discovery+redirect

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
