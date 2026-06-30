# Xal-Tor-Ka — Provider di autenticazione esterni (OIDC)

Oltre al login locale (password + TOTP), Xal-Tor-Ka può delegare l'autenticazione
a un **provider OpenID Connect** esterno: l'utente clicca «Accedi con Google /
Microsoft / …», si autentica sul provider (che gestisce anche l'eventuale MFA) e
torna autenticato. Sono supportati tutti gli IdP conformi a OIDC con discovery
(`/.well-known/openid-configuration`): **Google**, **Microsoft (Entra ID)** e i
generici **Keycloak, Authentik, Auth0, Okta, GitLab**, ecc.

> **Come funziona (in breve).** Xal-Tor-Ka usa l'*Authorization Code Flow*:
> redirige al provider con `state`+`nonce` (anti-CSRF/replay), riceve il codice sul
> proprio endpoint di callback, lo scambia con un `id_token` JWT, ne **verifica la
> firma** sulle chiavi pubbliche del provider (JWKS) e legge l'email. È
> **fail-closed**: qualunque errore nel giro nega il login, non lo concede.

---

## 0. Concetti chiave (validi per tutti i provider)

| Concetto | Valore in Xal-Tor-Ka |
|----------|----------------------|
| **Redirect URI** (da registrare sul provider) | `<GATE_URL>/auth/<id>/callback` |
| **Issuer** | URL base del provider (vedi sotto per provider) |
| **id provider** | la chiave in `config.json` (`google`, `microsoft`, o uno scelto da te) |
| **client_id** | rilasciato dal provider → in `config.json` |
| **client_secret** | rilasciato dal provider → in **`secrets.json`** (mai in config.json, mai in git) |

Esempio di redirect URI con `GATE_URL=https://gate.tuodominio.it`:

```
https://gate.tuodominio.it/auth/google/callback
https://gate.tuodominio.it/auth/microsoft/callback
```

> ⚠️ Il redirect URI **deve combaciare esattamente** con quello registrato sul
> provider (schema, host, porta, path). Deriva da `server.external_url` (`GATE_URL`):
> se quello è sbagliato, il provider rifiuta con `redirect_uri_mismatch`.

### Nessun auto-provisioning (per sicurezza)

Autenticarsi con Google **non basta** per entrare: l'utente deve **esistere già**
in Xal-Tor-Ka ed essere dichiarato per **quel** provider. Altrimenti chiunque abbia
un account Google potrebbe loggarsi. Crea l'utente con l'email dell'IdP e il
provider corrispondente:

```bash
# utente OIDC (niente password: l'autenticazione la fa il provider)
docker compose run --rm xaltorka user \
  --config /etc/xaltorka \
  --email mario.rossi@gmail.com --provider google
# eventualmente promuovi ad admin: aggiungi --admin
```

Poi, da `/admin → Utenti → (l'utente) → Proprietà`, assegna le **autorizzazioni**
ai backend `whitelist` (gli admin accedono a tutto). Le autorizzazioni vivono in
`users.json` come per gli utenti locali.

> L'email restituita dall'IdP deve combaciare con `email` dell'utente. Per
> Microsoft, se manca il claim `email`, si usa `preferred_username` (di norma l'UPN).

---

## 1. Attivazione (procedura generale, 4 passi)

1. **Registra l'app** sul provider (sezioni 2–4) e ottieni `client_id` +
   `client_secret`, impostando il redirect URI `<GATE_URL>/auth/<id>/callback`.
2. **`config.json`** → metti `enabled: true`, `client_id` e (per OIDC generici)
   l'`issuer` corretto:
   ```json
   { "id": "google", "type": "oidc", "name": "Google", "enabled": true,
     "issuer": "https://accounts.google.com", "client_id": "1234...apps.googleusercontent.com" }
   ```
3. **`secrets.json`** → metti il client secret sotto la stessa chiave id:
   ```json
   { "providers": { "google": { "client_secret": "GOCSPX-..." } } }
   ```
4. **Ricostruisci** (lo schema/config è cambiato → serve il binario aggiornato):
   ```bash
   make rebuild        # = docker compose up -d --build
   make logs           # cerca: "oidc provider abilitato" id=google ...
   ```

Da qui, nella pagina `/login` compare il bottone **«Accedi con Google»**. Crea gli
utenti OIDC (vedi sopra) e provali.

> La validazione è rigida (Fail-Fast all'avvio): un provider `oidc` con
> `enabled:true` **richiede** `issuer` e `client_id`, altrimenti il servizio si
> rifiuta di partire con un messaggio chiaro.

---

## 2. Google

1. Vai su **Google Cloud Console** → *APIs & Services* → **Credentials**
   (`https://console.cloud.google.com/apis/credentials`).
2. Configura prima la **OAuth consent screen** (tipo *External* se usi account
   Gmail generici; *Internal* se solo Google Workspace della tua organizzazione).
3. **Create Credentials → OAuth client ID** → *Application type:* **Web application**.
4. In **Authorized redirect URIs** aggiungi:
   `https://gate.tuodominio.it/auth/google/callback`
5. Copia **Client ID** e **Client secret**.
6. In `config.json` (provider `google`): `enabled:true`, `client_id:<...>`,
   `issuer:"https://accounts.google.com"`. In `secrets.json`:
   `providers.google.client_secret:<...>`.

Google restituisce `email` con `email_verified` → l'identità è affidabile.

---

## 3. Microsoft (Entra ID / Azure AD)

1. Portale **Microsoft Entra admin center** (`https://entra.microsoft.com`) o Azure
   Portal → **App registrations** → **New registration**.
2. **Redirect URI**: tipo *Web* → `https://gate.tuodominio.it/auth/microsoft/callback`.
3. Dalla pagina **Overview** copia **Application (client) ID** e **Directory
   (tenant) ID**.
4. **Certificates & secrets → New client secret** → copia il *Value* (non l'ID).
5. In `config.json` (provider `microsoft`):
   - `enabled:true`, `client_id:<Application ID>`
   - `issuer:"https://login.microsoftonline.com/<TENANT_ID>/v2.0"`
     (sostituisci `<TENANT_ID>` con il **Directory (tenant) ID**).
6. In `secrets.json`: `providers.microsoft.client_secret:<Value>`.

> ⚠️ **Usa il tenant specifico**, non `common`. Per le app multi-tenant l'issuer di
> discovery contiene il placeholder `{tenantid}` e la verifica dell'`id_token`
> fallirebbe. Con il tenant ID esplicito (single-tenant) tutto torna. Se ti serve
> davvero il multi-tenant, va gestito a parte (validazione issuer custom).

---

## 4. Provider OIDC generici (Keycloak, Authentik, Auth0, Okta, GitLab…)

Funzionano tutti allo stesso modo: scegli un `id` libero (es. `keycloak`), registra
un client *Web/Confidential* con redirect URI `<GATE_URL>/auth/<id>/callback`,
e usa come `issuer` l'URL base del provider. Il discovery deve rispondere a
`<issuer>/.well-known/openid-configuration`.

| Provider | Issuer tipico |
|----------|---------------|
| **Keycloak** | `https://kc.tuodominio.it/realms/<realm>` |
| **Authentik** | `https://auth.tuodominio.it/application/o/<app-slug>/` |
| **Auth0** | `https://<tenant>.eu.auth0.com/` |
| **Okta** | `https://<org>.okta.com/` (o `…/oauth2/default`) |
| **GitLab** | `https://gitlab.com` (o l'URL della tua istanza) |

Esempio `config.json`:

```json
{ "id": "keycloak", "type": "oidc", "name": "SSO aziendale", "enabled": true,
  "issuer": "https://kc.tuodominio.it/realms/intranet",
  "client_id": "xaltorka" }
```

`secrets.json`: `providers.keycloak.client_secret:<...>`. Crea gli utenti con
`--provider keycloak`.

> **GitHub** *non* è OIDC standard (non espone un discovery/`id_token` per il login):
> richiederebbe un handler OAuth2 dedicato. Non è supportato da questa via; usa un
> IdP OIDC davanti a GitHub (es. Keycloak con identity broker) se necessario.

---

## 5. SSO tra sottodomini (nota importante)

Il redirect URI di callback è **fisso** (l'host di `GATE_URL`), perché va registrato
sul provider. Dopo il callback, il cookie di sessione viene posato su quell'host.

- **In sviluppo** (`*.localhost`, cookie host-only) la sessione OIDC vale sull'host
  del gate, non automaticamente sugli altri vhost.
- **In produzione**, per avere **SSO** su tutti i sottodomini (`app1.dominio`,
  `app2.dominio`, …) imposta `session.cookie_domain` al **dominio padre**
  (`.tuodominio.it`): così il cookie è condiviso tra i sottodomini. Vedi
  `session.cookie_domain` in `config.json` (env `COOKIE_DOMAIN`).

---

## 6. Troubleshooting

| Sintomo | Causa probabile |
|---------|-----------------|
| `redirect_uri_mismatch` sul provider | redirect URI non identico a quello registrato; controlla `GATE_URL`/`external_url` (schema+host+path) |
| Login torna con «provider non disponibile» | discovery fallita: `issuer` errato o IdP irraggiungibile dal container; verifica egress di rete |
| «utente non abilitato: \<email\>» | l'utente non esiste o ha un `provider` diverso; crealo con `user --email … --provider <id>` |
| «verifica anti-CSRF fallita» / «sessione scaduta» | cookie `xtk_oidc` perso (oltre 10 min tra start e callback, o cookie bloccati) — riparti dal login |
| Microsoft: login ok ma email vuota | abilita il claim opzionale `email` nella app, oppure ci si appoggia a `preferred_username` |
| Il bottone non compare in `/login` | provider `enabled:false`, oppure non hai fatto `rebuild` dopo aver toccato `config.json` |

I tentativi falliti (incluso OIDC) finiscono in `logs/auth.log` con
`event=oidc …` → integrabili in **fail2ban** (vedi `INSTALL.md` §8).
