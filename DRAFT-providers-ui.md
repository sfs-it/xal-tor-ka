# BOZZA — Gestione provider OIDC in `/admin` (da rivedere)

> Draft di discussione, **non** committato/pushato. Obiettivo: gestire i provider
> di login esterni (Google, Microsoft, OIDC generici) dall'interfaccia, invece di
> editare a mano `config.json` + `secrets.json` e ricostruire.

## Stato attuale (da cui partiamo)
- Provider **non secret** in `config.json` → `providers: [{id,type,name,enabled,issuer,client_id}]`
  (env-templated, letto **solo all'avvio**, non hot-reloadable → cambio = `make rebuild`).
- `client_secret` in `secrets.json` → `providers.<id>.client_secret` (mai versionato).
- La mappa OIDC viene costruita **una volta** all'avvio (`buildOIDC` in `main.go`) e
  messa in `s.OIDC`. Discovery **lazy** al primo uso.

## Idea di fondo
Fare coi provider ciò che abbiamo fatto con servizi/monitor/admin-IP: **spostare la
parte editabile a runtime** in `services.json` (hot-reload), tenendo il `client_secret`
in `secrets.json` (600, mai in git) ma **gestito dalla UI in modo write-only**.

- `services.json` → nuovo campo `providers: []ProviderCfg` (runtime, hot).
- Merge all'avvio/reload: `config.json.providers` (base: `local` + esempi) **+**
  `services.json.providers`.
- On save → **ricostruire `s.OIDC`** a caldo (lock + rebuild), niente restart.
  Serve rendere `s.OIDC` swappabile sotto `RWMutex` (oggi è una mappa fissa) e leggere
  sotto `RLock` in `oidcButtons`/`handleOIDCStart|Callback`.

## Pagina `/admin/providers` (mockup)
```
Provider di autenticazione
┌───────────────────────────────────────────────────────────────────────────┐
│ id         name        tipo   attivo   issuer                    stato       │
│ google     Google      oidc     ●      accounts.google.com        ✓ ok       │ [modifica][test][disabilita][elimina]
│ microsoft  Microsoft   oidc     ○      login.microsoftonline…/…   – (off)     │ [modifica][test][abilita][elimina]
└───────────────────────────────────────────────────────────────────────────┘

Aggiungi / modifica provider
  preset:  [Google ▾]   ( Google | Microsoft/Entra | OIDC generico )
  id:      [google]                 name: [Google]
  issuer:  [https://accounts.google.com]
  client_id:     [1234…apps.googleusercontent.com]
  client_secret: [•••• impostato — lascia vuoto per non cambiarlo]   attivo [☑]

  Redirect URI da registrare sull'IdP:
     https://gate.tuodominio.it/auth/google/callback     [copia ⧉]

  [ prova discovery ]   [ salva ]
```

### Comportamenti chiave
- **`client_secret` write-only**: non viene MAI rimandato al browser; mostra solo
  "impostato/non impostato". Campo vuoto in edit = mantieni quello esistente.
- **Redirect URI** calcolato da `external_url` → `<GATE_URL>/auth/<id>/callback`,
  mostrato con bottone "copia" (va incollato nella console del provider).
- **Bottone "prova discovery"**: fa la discovery contro l'`issuer` (chiama
  `ensure()`), riporta ✓/✗ **prima** di abilitare. Consiglio: **non abilitare** un
  provider se la discovery fallisce (fail-closed anche in UI).
- **Preset** che pre-riempiono l'issuer: Google (`accounts.google.com`), Microsoft
  (`login.microsoftonline.com/<TENANT_ID>/v2.0`, con avviso single-tenant), generico.
- Link "crea utente per questo provider" → si aggancia al TODO "selettore provider
  nel form crea-utente" (gli utenti OIDC non hanno password).

## Endpoint previsti
- `GET  /admin/providers`            pagina
- `POST /admin/provider/add`         id,name,issuer,client_id,client_secret,enabled
- `POST /admin/provider/edit`        (secret vuoto = invariato)
- `POST /admin/provider/del`
- `POST /admin/provider/toggle`
- `POST /admin/provider/test`        discovery check → ✓/✗ (no persistenza)

## Persistenza & reload
- Non-secret → `services.json.providers` (via `mutateServices` + snapshot).
- Secret → `secrets.json` (via `SaveSecrets`, atomico+snapshot, 600).
- `Reload()` (già hot) chiama un nuovo `rebuildOIDC()` che rilegge merge+secret e
  **swappa `s.OIDC`** sotto lock. `config.Validate` estesa a validare i provider
  runtime (oidc abilitato ⇒ issuer+client_id).

## Sicurezza / note
- Secret mai loggato né reso; solo stato "set". Redirect URI fisso (registrato).
- Cambiare l'`id` di un provider esistente = rischio "utenti orfani" (gli utenti
  hanno `provider=<id>`): meglio id **non modificabile** in edit (come per i backend).
- Multi-tenant Microsoft (`common`) resta un caso a parte (validazione issuer custom
  — già a backlog).

## Domande aperte per domani
1. Provider runtime in `services.json` (hot) **oppure** restare in `config.json` (rebuild)?
   → proposta: `services.json`, per coerenza con servizi/monitor.
2. Hot-swap di `s.OIDC` sotto lock: ok l'approccio? (piccolo refactor di `s.OIDC` in campo guardato)
3. "Abilita" richiede discovery ok? (consigliato)
4. i18n: le nuove stringhe entrano nel catalogo (in EN + 9 lingue) come il resto.

## Input utente (da altro sistema) — vhost di AUTH centralizzato + callback unica
Pattern SSO classico da valutare come **modalità produzione**:
- Un **vhost dedicato all'auth** (es. `auth.dominio`) ospita `/login`, `/logout` e
  **la callback OIDC** `/auth/<provider>/callback`.
- I vhost protetti: `auth_request` → su **401** fanno `error_page 401` → redirect a
  `https://auth.dominio/login?next=<url-originale>` (invece di servire il login in
  locale su ogni host, come ora in dev).
- Login su `auth.dominio` → cookie di sessione sul **dominio padre** `.dominio`
  (`session.cookie_domain`) → **SSO** su tutti i sottodomini.
- **Redirect URI OIDC unica e fissa**: `https://auth.dominio/auth/<id>/callback`,
  registrata una sola volta su Google/Microsoft. Semplifica enormemente.

Mappatura su Xal-Tor-Ka:
- È di fatto la nostra "modalità prod": `cookie_domain=.dominio` + un **"auth host"
  configurabile** (chi serve login/callback). Oggi facciamo per-host (workaround
  `*.localhost` in dev); in prod con dominio vero conviene centralizzare.
- Impatti: (a) `redirect URI` del provider deve puntare all'auth host, non al vhost;
  (b) la generazione NGINX dei vhost protetti deve emettere `error_page 401` →
  `https://<auth-host>/login?next=…`; (c) `sanitizeNext` deve accettare il ritorno
  cross-subdomain verso i backend noti (già previsto in parte).
- Da decidere: introdurre un `auth_host` esplicito in config, o riusare `external_url`
  come host di auth. In dev resta il fallback per-host.

## Input utente — provider ammessi PER SERVIZIO (osservazione dall'edit servizio)
Oggi `authenticated` = "sessione valida (2FA)", a prescindere da COME l'utente si è
loggato (locale o OIDC). Richiesta: poter vincolare **per singolo servizio** quali
provider sono accettati (es. "solo Google", "solo locale", "qualsiasi").
- Modello possibile: campo opzionale `auth_providers []string` sulla Route/Backend
  (vuoto = qualsiasi). In `/validate`, per regola `authenticated`/`whitelist`,
  verificare che `sess.Provider ∈ auth_providers` (se non vuoto), altrimenti 403/redirect.
- UI: nel form servizio, quando regola = authenticated/whitelist, mostrare uno
  **switch/multiselect dei provider ammessi** (popolato dai provider abilitati).
- Da chiarire con l'utente: vincolo a **un** provider o a **più** (multiselect)?
  E se un servizio richiede es. Google, un utente locale che tenta → messaggio chiaro.
