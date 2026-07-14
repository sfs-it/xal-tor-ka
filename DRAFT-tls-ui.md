# BOZZA — Rilascio/gestione certificati dal gatekeeper (da rivedere)

> Draft di discussione, **non** committato. Perché qui: il gate è l'unico edge che
> termina TLS e conosce già tutti gli host serviti (genera i vhost NGINX). Chiude i
> `TLS_MODE` del BLUEPRINT (`selfsigned`/`acme`) come feature gestita con UI.

## Due strade coesistenti
1. **ACME / Let's Encrypt (host pubblici)** — HTTP-01: NGINX serve
   `/.well-known/acme-challenge/` (proxato a un handler Go); il servizio Go ottiene e
   **rinnova** i cert (lib `lego` o `golang.org/x/crypto/acme`), li scrive in
   `certs/<host>.crt|key`, il vhost li referenzia, poi `reload`. Auto-rinnovo con
   goroutine legata al ctx (come health checker).
2. **CA interna / self-signed (LAN, dev, host senza DNS pubblico)** — il gate fa da
   mini-CA: genera CA, emette cert per-host, espone la CA da scaricare
   (`/admin/tls/ca.crt`) da installare sui client. Già mezzo previsto (`selfsigned` +
   `/setup/ca.crt`).

## UI `/admin/tls` (mockup)
```
Certificati
 host                  fonte          scadenza     stato      azioni
 app.dominio.it        Let's Encrypt  2026-09-30   ✓ valido   [rinnova]
 intranet.dominio.it   CA interna     2027-01-01   ✓ valido   [ri-emetti]
 router.local          —              —            mancante   [emetti LE][self-signed]
 [ Scarica CA interna ]   [ email ACME: ____ ]  [ TOS ☑ ]
```

## Caveat
- ACME/LE: host deve risolvere **pubblicamente al gate** + **porta 80 aperta**
  (HTTP-01). In dev/`*.localhost` → CA interna. Attenzione ai **rate-limit** LE.
- Sposta TLS da `external` a **gestito-dal-gate**: il gate termina HTTPS / pilota la
  config TLS di NGINX. Resta `external` per chi ha un LB a monte.
- Cert per l'**auth-host centralizzato** + cert dei servizi (vedi DRAFT-providers-ui).

## Da decidere domani
1. Libreria ACME: `lego` (completa, DNS providers) vs `x/crypto/acme` (stdlib, minimale)?
2. Challenge: HTTP-01 (semplice, richiede :80) vs DNS-01 (wildcard, serve provider DNS
   — già a backlog PowerDNS)?
3. Storage cert in `certs/` (già gitignored) + naming; rinnovo automatico a soglia giorni.
4. Chi termina TLS: NGINX legge `certs/<host>` (il Go scrive+reload) — coerente con
   l'attuale proxy manager.
5. i18n delle nuove stringhe (EN + 9 lingue).
