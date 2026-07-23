# Xal-Tor-Ka — cos'è, e perché metterlo davanti a (quasi) tutto

> Un **gatekeeper**: una sola porta d'ingresso che **autentica** una volta e fa da
> **reverse-proxy** verso molti servizi interni, con **regole d'accesso per-servizio**,
> **TLS**, e un **pannello** per gestirlo. Go + Docker + NGINX, self-contained, multilingua.

---

## Il problema

I servizi interni nascono quasi sempre **senza autenticazione seria**: una dashboard di
metriche, un'API di sviluppo, un pannello admin, un `phpMyAdmin`, un **LLM come Ollama**. Finché
restano su `localhost` va bene; nel momento in cui li esponi — anche solo su una LAN o su un IP —
diventano superficie d'attacco.

Non è teoria: nel 2025-26 ricercatori hanno trovato **~175.000 istanze Ollama esposte** senza
alcuna auth, con una campagna di abuso attiva («LLMjacking») che ne rivende l'accesso. La causa
è sempre la stessa: *il servizio non ha auth, e qualcuno lo ha pubblicato così com'è.*

La soluzione documentata e raccomandata è **mettere davanti un reverse-proxy autenticante** —
che concentra al bordo le cose che contano: **TLS, autenticazione, timeout, rate-limit, log**.
Il punto è che farlo a mano (nginx + oauth2-proxy + Authelia + certbot + …) diventa una
**catena di pezzi da incollare e mantenere**. Xal-Tor-Ka è quella catena, già montata e
gestibile da UI.

## Cosa fa

- **Autenticazione pluggable**: password **locali** (argon2id), **OIDC** (Google/Microsoft/
  generico), **LDAP / Active Directory** *(dalla beta0.6)*, **PAM** *(in roadmap)*.
- **Reverse-proxy gestito da pannello**: aggiungi un backend (`host → upstream`) e scegli la
  **regola d'accesso**: `public` · `authenticated` (login+2FA) · `authorized` (solo utenti
  autorizzati). Niente file nginx a mano.
- **TLS completo**: CA interna + self-signed per LAN/dev, **Let's Encrypt (ACME)** per i domini
  pubblici, gestione certificati dal pannello (incluso `www.`).
- **Knob NGINX per-vhost**: `client_max_body_size`, `proxy_timeout`, `no_buffering` (streaming/
  SSE — essenziale davanti a un LLM), WebSocket, upstream self-signed.
- **2FA (TOTP)**, sessioni, IP-whitelist per l'area admin.
- **Health monitoring** dei backend, **UI in 10 lingue** (incl. RTL).
- **Piattaforma di hosting** (estensione): siti isolati **docker-per-vhost**, utenti OS isolati,
  DB condivisi, **gateway SCP con chroot per-sito**, agente host **blindato** (comandi vettati,
  non iniettabili).
- **Notifiche e controllo remoto** (Telegram/email, fail-closed).

## Perché adottarlo — casi d'uso

1. **Mettere l'auth davanti a un servizio che non ce l'ha.** Ollama, una dashboard, un'API
   interna, `phpMyAdmin`: diventano un **backend** con regola `authenticated` o `whitelist` +
   TLS, in minuti. (Vedi `docs/`/report *ollama-gatekeeper*.)
2. **Homelab / PMI**: un reverse-proxy con **auth vera** e certificati, senza incollare 4 tool.
3. **Intranet multi-servizio**: **un solo login**, molti servizi dietro, accesso deciso
   per-servizio. Gli utenti vedono una dashboard con solo ciò a cui hanno diritto.
4. **Hosting leggero**: siti in docker isolate con SCP/SFTP chrootato, senza Plesk/Virtualmin.
5. **SSO enterprise davanti al legacy**: OIDC oggi, **LDAP/Active Directory** dalla beta0.6 —
   autentichi con gli **account di dominio** e (con i gruppi) mappi chi accede a cosa.

## Perché *questo* e non un altro pezzo

| Vuoi… | Da solo | Con Xal-Tor-Ka |
|---|---|---|
| routing | nginx/Traefik a mano | backend da UI |
| auth al bordo | oauth2-proxy/Authelia (glue esterno) | integrata (local/OIDC/LDAP) |
| TLS | certbot/CA a mano | ACME + CA interna dal pannello |
| streaming LLM | tuning nginx a mano | knob `no_buffering`/`timeout` per-vhost |
| gestione | file di config | **pannello unico**, multilingua |

Non sostituisce Traefik per l'orchestrazione a grande scala; **vince quando vuoi
autenticazione + reverse-proxy + gestione in un solo pezzo**, con isolamento e sicurezza di
default.

## Principi di disegno

- **Sicurezza di default**: isolamento forte (docker-per-sito, utenti OS), agente host con soli
  **comandi vettati non-iniettabili**, nessun segreto nei commit (repo pubblico).
- **Self-contained**: Go statico + Docker + NGINX; nessun database esterno obbligatorio (store
  su file), deploy in `docker compose up`.
- **Gestibile da UI**: la configurazione runtime vive in `services.json` (hot-reload), non in
  archeologia di file.
- **Multilingua** (10 lingue) e **accessibile**.

## Partenza

`INSTALL.md` per il deploy (docker o host). In sintesi: `docker compose up -d --build`, crei
l'admin, aggiungi un backend, scegli la regola, emetti il certificato. ~30 minuti dal nulla a un
gate funzionante.

---

*Xal-Tor-Ka — il guardiano del cancello. SFS.it · Apache-2.0.*
