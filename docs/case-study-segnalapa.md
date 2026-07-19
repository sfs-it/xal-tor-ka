# Caso di studio — SegnalaPa su Xal-Tor-Ka (hosting multi-vhost + SSO)

> Documento vivo. Raccoglie le **richieste** del progetto SegnalaPa (cliente conto-terzi,
> team-leader deputato da Mannie, coordinato via bottiglia) e come la piattaforma le
> soddisfa — così le esigenze che emergono da un cliente reale diventano requisiti di
> prodotto tracciati, non sapere che muore in una finestra di contesto.
> Autore: custode. Aggiornato: 2026-07-17.

## 1. Il cliente e la forma del bisogno
SegnalaPa è un servizio con **più componenti sullo stesso dominio** `segnalapa.it`:
- **vetrina** statica → `segnalapa.it` / `www.` (LIVE: container `segnalapa-web`, TLS valido).
- **PWA** → `app.segnalapa.it` (sorgenti conto-terzi, in consegna).
- **API Laravel** → `api.segnalapa.it` (sorgenti in consegna) + **pgsql condiviso** (2 DB/utenti
  isolati, `segnalapa_auth`/`_core`, postgis dormiente).
- **SSO** → ipotesi `auth.segnalapa.it` (autenticazione sul dominio del cliente per evitare i
  problemi di cookie cross-domain — la "via pulita" del Fondatore: un vhost auth per dominio).

Modello piattaforma: **un sito = un utente OS** (`site-segnalapa`, chroot SCP porta 2222) con
**N vhost**, ciascuno la sua docker; upstream interni `<vhost>.site:8080` sulla rete `xtk-hosting`.

## 2. Requisiti di piattaforma che ne derivano (tracciati)
1. **Multi-vhost per sito** con dominio pubblico per-vhost. ✅ implementato.
2. **Cert TLS per ogni vhost secondario** (`app.`/`api.`). ⚠️ vedi §3 — funziona, ma con un
   prerequisito non ovvio + un gap di UX.
3. **SSO cross-domain** (`auth.<dominio-cliente>`). ⏳ decisione di design aperta (vedi §4).
4. **Siti "public"/pass-through** che NON ereditano i path riservati del gate (`/assets`,
   `/login`…). ⚠️ bug noto (beta0.7) — un sito public con `/assets/` collide col core.
5. **DB condivisi** pg/mysql con utenti isolati per cliente. ✅ (pg da provisionare per api).

## 3. Certificati dei vhost secondari — come funziona davvero
**Il cert segue il backend, non viceversa** (by design). La pagina `/admin/tls` emette solo per
gli **host "serviti"** (`servedHosts()`, con un backend nel resolver). Quindi:

Flusso corretto per `app.segnalapa.it`:
1. **Hosting → sito → vhost → Publish** con `domain=app.segnalapa.it` → crea il backend +
   rigenera il `server_name` nginx.
2. **SSL / TLS → Issue LE**. La challenge ACME HTTP-01 la risponde **il gateway su :80** (non il
   container) → **si può certificare anche a container spento**, purché:
   - il vhost sia **pubblicato** (server_name presente), e
   - il **DNS del sottodominio punti al VPS**.

Stato DNS (verificato 2026-07-17, IP VPS `213.152.202.118`):
- `app.segnalapa.it` → punta ✅ (certificabile subito).
- `api.segnalapa.it` → **nessun record A** ❌ (il TL deve aggiungere l'A record prima di ACME).
- `auth.segnalapa.it` → punta, ma nessun vhost → cade sul `default_server` (vedi §4).

**Nota per evitare il 502**: finché i sorgenti non atterrano, si può pubblicare `app.`/`api.`
puntando temporaneamente l'upstream alla vetrina statica (holding page 200 + cert valido), poi
scambiare l'upstream quando i coder consegnano.

**Gap di UX (non di funzione):** l'utente cerca i cert nella pagina TLS, che però non mostra gli
host non pubblicati → sembra "impossibile". Manca un ponte *"pubblica per abilitare SSL"* o una
scorciatoia Publish+SSL per-vhost. → miglioramento proposto.

**MA la causa profonda era un bug di piattaforma (2026-07-17):** anche pubblicando, nulla arrivava
a nginx perché la dir `/opt/xaltorka/nginx/conf.d` era `root:root` e il core (uid 1000) non poteva
rigenerare `backends.conf` (write atomica → serve write sulla dir). Fix: `chown 1000:1000`. Dopo
il fix, il flusso completo ha funzionato: **`app.segnalapa.it` pubblicato + cert Let's Encrypt
emesso e servito** (scad. 2026-10-15). Vedi guardrail `deploy-confd-writable-by-core`. Verifica
end-to-end fatta *a vista* (login headless + screenshot) e via `openssl s_client`.

## 4. `auth.segnalapa.it` → gatekeeper: accidentale, non SSO
Non esiste un vhost `auth.segnalapa.it`. Il sottodominio punta al VPS ma, **senza server_name
proprio, nginx lo manda al `default_server` = il gateway** → l'utente vede il login di
Xal-Tor-Ka. **Non è un SSO configurato**, è il catch-all. Due strade (decisione SegnalaPa):
- **vogliono SSO** → si pubblica `auth.segnalapa.it` come vhost auth dedicato (via pulita).
- **non lo vogliono** → serve l'**hardening del `default_server`**: gli host sconosciuti non
  devono ricevere il login (meglio 444/holding neutro), così un sottodominio orfano non sembra
  "roba nostra". Vale per OGNI cliente, non solo SegnalaPa.

## 5. Incidenti di produzione (deploy hygiene) — lezioni
Nella stessa infrastruttura, il 2026-07-17 due regressioni silenziose (dagli update beta0.6):
- **pannello hosting 404**: core ricreato senza l'overlay → `HOSTING_UPSTREAM` perso.
  Fix: `COMPOSE_FILE` sticky nel `.env`. → guardrail `hosting-overlay-sticky-compose-file`.
- **pannello "No sites"**: command script agente a 775 → rifiutati (fail-closed) → `site_list` ko.
  Fix: `chmod 0755`. → guardrail `deploy-agent-scripts-chown-root` (sez. permessi).
Lezione trasversale: **in produzione il degrado silenzioso è il nemico** — lo stato desiderato
dev'essere riproducibile da un comando "nudo", e la verità sta nei log, non nell'UI che tace.

## 6. Prossime azioni
- [x] **`app.segnalapa.it`: publish + cert LE emesso e servito** (2026-07-17). Upstream = suo
      container `segnalapa-app` (static, avviato; docroot vuoto finché non arriva la PWA).
- [ ] `api.segnalapa.it`: **manca il CNAME/A record** (il TL deve aggiungerlo); poi publish + cert
      + pgsql/DB. Senza DNS l'ACME non parte.
- [ ] `auth.segnalapa.it`: chiarire l'**intento SSO** col TL (oggi cade sul default_server).
- [ ] Piattaforma: **fix deploy** — chown `nginx/conf.d` al core uid a ogni deploy
      (guardrail `deploy-confd-writable-by-core`); e al boot **alert** se backends.conf non scrivibile.
- [ ] Piattaforma: **hardening `default_server`** (host sconosciuti → no login).
- [ ] Piattaforma: **ponte UX** Publish→SSL per vhost non pubblicati.
- [ ] Pulizia: `app.segnalapa.it` pubblicato con `www=1` → server_name `www.app.segnalapa.it`
      (innocuo, senza DNS); togliere `www` dal backend quando si rifinisce.
- [ ] beta0.7: fix `public` pass-through (§2.4).
