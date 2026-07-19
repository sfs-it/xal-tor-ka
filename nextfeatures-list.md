# Xal-Tor-Ka — next features (mini-blueprint di lavoro)

> Roadmap operativa delle prossime feature. **Flusso di lavoro concordato:**
> spec qui → (per le UI) **mockup "tarocco"** per approvazione → coding sull'approvato →
> verifica su localhost / batch → deploy → ritocchini. Ogni voce ha:
> *cosa · perché · cosa c'è già · come · sforzo · priorità · mockup?*
> Autore: custode. Aggiornato: 2026-07-18.

Legenda sforzo: 🟢 piccolo · 🟡 medio · 🔴 grande. Priorità: **P1** (prossimo) · **P2** · **P3**.

---

## A. Proxy / accesso

### A1 — Override auth per-path (opt-in) · P1 · 🟢 · mockup: sì
**Cosa.** Su un singolo servizio, proteggere con auth **path/file specifici** lasciando il resto
public (es. `wp-login.php` e `/wp-admin/` dietro Google, `/` pubblico).
**Perché.** Difesa in profondità: i bot non raggiungono nemmeno la login di WP. Utile per molti siti.
**Cosa c'è già.** Il motore lo fa: `Backend.Routes []Route{Path,Rule,Upstream}`, e `proxy/generate.go`
genera una `location` per route con `auth_request` sulle non-public. La lista mostra già più tag-regola.
**Come.** Sezione **opt-in** "Regole per path" nel form del backend: righe `path + regola` (aggiungi/togli),
upstream ereditato dal servizio. Match esatto `=` per i file, prefisso per le cartelle. `handleBackendEdit`
costruisce `Routes` (main `/` + override). Non cambia il default (chi non la tocca resta a route singola).

### A2 — Manopole reverse-proxy upstream nel form · P2 · 🟡 · mockup: sì
**Cosa.** Esporre nel form le opzioni proxy per upstream "difficili" (upstream HTTPS/Plesk/intranet):
`proxy_ssl_verify off`, header Upgrade/Connection, `X-Forwarded-*`, timeouts, `proxy_buffering off`.
**Perché.** Oggi si fa col blocco nginx custom grezzo; per casi tipo Plesk→intranet servono friendly.
**Cosa c'è già.** `Nginx.CustomServer/CustomLocation`, WebSocket, MaxBodyMB. Manca l'esposizione UI.
**Come.** Checkbox/campi nel form → mappati sugli opts nginx già supportati da `generate.go`.
**Nota "edge a monte":** un reverse-proxy DAVANTI al gate è già gestito (`trusted_proxies` + `X-Forwarded`).

---

## B. Sicurezza (il "trittico")

### B1 — fail2ban (jail host) · P1 · 🟡 · mockup: no (o piccolo pannello stato)
**Cosa.** Bannare al firewall gli IP con troppi fallimenti auth.
**Cosa c'è già.** Il gate **scrive il log fallimenti** (`auth_log`, apposta per fail2ban) + **bruteforce in
RAM** sul login. Manca solo il jail host.
**Come.** Comando **vettato dell'agente**: installa/gestisci un jail fail2ban (filter sul gate log + action
nftables). Pannello stato opzionale (IP bannati, unban). Difesa a strati: RAM (gate) + IP-ban (firewall).

### B2 — WAF (ModSecurity + OWASP CRS) · P2 · 🔴 · mockup: sì (toggle + pannello)
**Cosa.** Web Application Firewall generico (SQLi, XSS, path traversal…) davanti ai servizi, **toggle per-sito**.
**Cosa c'è già.** Nulla — è il pezzo nuovo vero.
**Come.** Immagine nginx esposto con **ModSecurity v3 + OWASP CRS**; flag `waf=on` per backend → include le
regole nel server-block. Modalità *detection-only* vs *blocking* per-sito. Log delle regole scattate.

### B3 — default_server hardening · P3 · 🟢 · mockup: no
**Cosa.** Un host sconosciuto puntato sul VPS non deve ricevere il **login del gate** (sembra "roba nostra").
**Come.** `default_server` → 444/holding neutro. (Nato dal caso `auth.segnalapa.it` catch-all.)

---

## C. Aggiornamenti OS (feature in corso)

### C1 — Notifica aggiornamenti (increment 2) · P1 · 🟡 · mockup: no
**Cosa.** Poller che controlla gli update ogni N ore e **notifica** (Telegram/email) secondo il flag.
**Cosa c'è già.** Comandi agente `os_updates_check/apply/preview/hold` (live), config `OSUpdatesCfg`
(`automation` off/notify/security/all · `notify` · `notify_on` any/security · `channels`), pannello Sistema.
**Come.** Poller nel core (usa `HostingUpstream` per il check via estensione + il notifier esistente);
`automation=security/all` applica via il comando apply. Mai auto-reboot.

### C2 — Pannello configurazione flag (increment 4) · P2 · 🟢 · mockup: sì
**Cosa.** UI admin per il flag: dropdown automazione, notifica on/off + any/security, canali, intervallo.
**Come.** Pagina/mini-form che scrive `OSUpdatesCfg` (runtime store) + ricarica il poller a caldo.

---

## D. SegnalaPa (cliente / collaborazione)

### D1 — Staging/preview interno · P2 · 🟡 · mockup: no
**Cosa.** Ambiente di preview per i servizi in dev, servito dal gate **senza esposizione pubblica**.
**Come (proposta).** Sottodominio `staging.segnalapa.it` regola `authenticated` (sull'HTTPS già esposto, ma
login-gated → non pubblico) + `noindex`; oppure `.lan` (solo LAN/VPN). Popolamento via SCP della build.
**Stato.** Proposto al TL in bottiglia; aspetta scelta forma + DNS.

### D2 — app./api./auth. + pgsql · P2 · 🟡 · dipende da loro
**Cosa.** Alzare `segnalapa-app` (PWA) e `segnalapa-api` (Laravel) + pgsql condiviso + vhost + cert.
**Stato.** `app.` DNS ok; `api.` manca DNS; sorgenti in consegna. `auth.` = decisione SSO (vedi E1).

---

## E. Auth avanzata

### E1 — SSO cross-domain (auth.<dominio> dedicato) · P3 · 🔴 · mockup: sì
**Cosa.** Login SSO sul dominio del cliente (`auth.segnalapa.it`) per evitare i problemi cookie cross-domain.
**Cosa c'è già.** Il filone del Fondatore (via `/xtk/` o vhost auth dedicato per dominio) — aperto.
**Come.** Vhost auth per-dominio + `session.cookie_domain`. Decisione di design con SegnalaPa.

### E2 — PAM come fonte auth · P3 · 🔴 · mockup: no
**Cosa.** Autenticazione via PAM (si sovrappone a LDAP per AD). **Cosa c'è già.** LDAP/AD implementato (beta0.6).
**Come.** cgo/libpam dietro l'interfaccia `Provider`.

---

## F. Igiene di piattaforma (debito noto)

### F1 — Deploy-script committato · P2 · 🟢 · mockup: no
**Cosa.** Uno script di deploy versionato che fonde le pezze deploy-hygiene di oggi: `chown` di
`nginx/conf.d` al core uid, `chmod 0755` degli script agente, `COMPOSE_FILE` sticky nel `.env`.
**Perché.** Le 4 regressioni di oggi (404 hosting, "No sites", conf.d, CSS segnalapa) erano tutte
deploy-hygiene: uno script unico le previene alla radice.

### F2 — Alert al boot se backends.conf non scrivibile · P3 · 🟢 · mockup: no
**Cosa.** Se il core non può rigenerare backends.conf → **alert rumoroso** (Telegram), non un WARN silenzioso.
**Perché.** Il degrado silenzioso in produzione è il nemico (lezione conf.d).

### F3 — TLS UX bridge · P3 · 🟢 · mockup: sì
**Cosa.** Nel TLS, per un host non pubblicato: hint "pubblica per abilitare SSL" / scorciatoia Publish+SSL.

---

## Ordine proposto
1. **A1** override per-path (piccolo, alto valore, sblocca wp-login)
2. **C1** notifica aggiornamenti OS (chiude la feature in corso)
3. **B1** fail2ban jail (gancio già presente)
4. poi **B2 WAF**, **C2**, **A2**, **D1** … secondo priorità.

*Mockup "tarocchi" richiesti per:* A1, A2, B2, C2, E1, F3.
