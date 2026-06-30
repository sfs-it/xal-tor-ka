# Xal-Tor-Ka — External authentication providers (OIDC)

Beyond local login (password + TOTP), Xal-Tor-Ka can delegate authentication
to an external **OpenID Connect provider**: the user clicks "Sign in with Google /
Microsoft / …", authenticates with the provider (which also handles any MFA) and
comes back authenticated. All OIDC-compliant IdPs with discovery
(`/.well-known/openid-configuration`) are supported: **Google**, **Microsoft (Entra ID)** and the
generic **Keycloak, Authentik, Auth0, Okta, GitLab**, etc.

> **How it works (in brief).** Xal-Tor-Ka uses the *Authorization Code Flow*:
> it redirects to the provider with `state`+`nonce` (anti-CSRF/replay), receives the code on its
> own callback endpoint, exchanges it for an `id_token` JWT, **verifies its
> signature** against the provider's public keys (JWKS) and reads the email. It is
> **fail-closed**: any error along the way denies the login, it does not grant it.

---

## 0. Key concepts (valid for all providers)

| Concept | Value in Xal-Tor-Ka |
|----------|----------------------|
| **Redirect URI** (to register on the provider) | `<GATE_URL>/auth/<id>/callback` |
| **Issuer** | provider base URL (see per-provider below) |
| **provider id** | the key in `config.json` (`google`, `microsoft`, or one you choose) |
| **client_id** | issued by the provider → in `config.json` |
| **client_secret** | issued by the provider → in **`secrets.json`** (never in config.json, never in git) |

Example redirect URI with `GATE_URL=https://gate.yourdomain.com`:

```
https://gate.yourdomain.com/auth/google/callback
https://gate.yourdomain.com/auth/microsoft/callback
```

> ⚠️ The redirect URI **must match exactly** the one registered on the
> provider (scheme, host, port, path). It is derived from `server.external_url` (`GATE_URL`):
> if that is wrong, the provider rejects with `redirect_uri_mismatch`.

### No auto-provisioning (for security)

Authenticating with Google **is not enough** to get in: the user must **already exist**
in Xal-Tor-Ka and be declared for **that** provider. Otherwise anyone with
a Google account could log in. Create the user with the IdP email and the
corresponding provider:

```bash
# OIDC user (no password: authentication is handled by the provider)
docker compose run --rm xaltorka user \
  --config /etc/xaltorka \
  --email mario.rossi@gmail.com --provider google
# optionally promote to admin: add --admin
```

Then, from `/admin → Users → (the user) → Properties`, assign the **authorizations**
to the `whitelist` backends (admins access everything). The authorizations live in
`users.json` just like for local users.

> The email returned by the IdP must match the user's `email`. For
> Microsoft, if the `email` claim is missing, `preferred_username` is used (usually the UPN).

---

## 1. Activation (general procedure, 4 steps)

1. **Register the app** on the provider (sections 2–4) and obtain `client_id` +
   `client_secret`, setting the redirect URI `<GATE_URL>/auth/<id>/callback`.
2. **`config.json`** → set `enabled: true`, `client_id` and (for generic OIDC)
   the correct `issuer`:
   ```json
   { "id": "google", "type": "oidc", "name": "Google", "enabled": true,
     "issuer": "https://accounts.google.com", "client_id": "1234...apps.googleusercontent.com" }
   ```
3. **`secrets.json`** → set the client secret under the same id key:
   ```json
   { "providers": { "google": { "client_secret": "GOCSPX-..." } } }
   ```
4. **Rebuild** (the schema/config has changed → the updated binary is needed):
   ```bash
   make rebuild        # = docker compose up -d --build
   make logs           # look for: "oidc provider abilitato" id=google ...
   ```

From here, the **"Sign in with Google"** button appears on the `/login` page. Create the
OIDC users (see above) and test them.

> Validation is strict (Fail-Fast at startup): an `oidc` provider with
> `enabled:true` **requires** `issuer` and `client_id`, otherwise the service
> refuses to start with a clear message.

---

## 2. Google

1. Go to **Google Cloud Console** → *APIs & Services* → **Credentials**
   (`https://console.cloud.google.com/apis/credentials`).
2. First configure the **OAuth consent screen** (type *External* if you use generic
   Gmail accounts; *Internal* if only your organization's Google Workspace).
3. **Create Credentials → OAuth client ID** → *Application type:* **Web application**.
4. Under **Authorized redirect URIs** add:
   `https://gate.yourdomain.com/auth/google/callback`
5. Copy the **Client ID** and **Client secret**.
6. In `config.json` (provider `google`): `enabled:true`, `client_id:<...>`,
   `issuer:"https://accounts.google.com"`. In `secrets.json`:
   `providers.google.client_secret:<...>`.

Google returns `email` with `email_verified` → the identity is trustworthy.

---

## 3. Microsoft (Entra ID / Azure AD)

1. **Microsoft Entra admin center** portal (`https://entra.microsoft.com`) or Azure
   Portal → **App registrations** → **New registration**.
2. **Redirect URI**: type *Web* → `https://gate.yourdomain.com/auth/microsoft/callback`.
3. From the **Overview** page copy the **Application (client) ID** and **Directory
   (tenant) ID**.
4. **Certificates & secrets → New client secret** → copy the *Value* (not the ID).
5. In `config.json` (provider `microsoft`):
   - `enabled:true`, `client_id:<Application ID>`
   - `issuer:"https://login.microsoftonline.com/<TENANT_ID>/v2.0"`
     (replace `<TENANT_ID>` with the **Directory (tenant) ID**).
6. In `secrets.json`: `providers.microsoft.client_secret:<Value>`.

> ⚠️ **Use the specific tenant**, not `common`. For multi-tenant apps the discovery
> issuer contains the placeholder `{tenantid}` and `id_token` verification
> would fail. With the explicit tenant ID (single-tenant) everything works. If you
> really need multi-tenant, it must be handled separately (custom issuer validation).

---

## 4. Generic OIDC providers (Keycloak, Authentik, Auth0, Okta, GitLab…)

They all work the same way: pick a free `id` (e.g. `keycloak`), register
a *Web/Confidential* client with redirect URI `<GATE_URL>/auth/<id>/callback`,
and use the provider's base URL as the `issuer`. Discovery must respond at
`<issuer>/.well-known/openid-configuration`.

| Provider | Typical issuer |
|----------|---------------|
| **Keycloak** | `https://kc.yourdomain.com/realms/<realm>` |
| **Authentik** | `https://auth.yourdomain.com/application/o/<app-slug>/` |
| **Auth0** | `https://<tenant>.eu.auth0.com/` |
| **Okta** | `https://<org>.okta.com/` (or `…/oauth2/default`) |
| **GitLab** | `https://gitlab.com` (or your instance's URL) |

`config.json` example:

```json
{ "id": "keycloak", "type": "oidc", "name": "Corporate SSO", "enabled": true,
  "issuer": "https://kc.yourdomain.com/realms/intranet",
  "client_id": "xaltorka" }
```

`secrets.json`: `providers.keycloak.client_secret:<...>`. Create the users with
`--provider keycloak`.

> **GitHub** is *not* standard OIDC (it does not expose a discovery/`id_token` for login):
> it would require a dedicated OAuth2 handler. It is not supported through this path; use an
> OIDC IdP in front of GitHub (e.g. Keycloak with identity broker) if needed.

---

## 5. SSO across subdomains (important note)

The callback redirect URI is **fixed** (the `GATE_URL` host), because it has to be registered
on the provider. After the callback, the session cookie is set on that host.

- **In development** (`*.localhost`, host-only cookie) the OIDC session is valid on the gate
  host, not automatically on the other vhosts.
- **In production**, to have **SSO** across all subdomains (`app1.dominio`,
  `app2.dominio`, …) set `session.cookie_domain` to the **parent domain**
  (`.yourdomain.com`): this way the cookie is shared among the subdomains. See
  `session.cookie_domain` in `config.json` (env `COOKIE_DOMAIN`).

---

## 6. Troubleshooting

| Symptom | Probable cause |
|---------|-----------------|
| `redirect_uri_mismatch` on the provider | redirect URI not identical to the registered one; check `GATE_URL`/`external_url` (scheme+host+path) |
| Login returns with "provider not available" | discovery failed: wrong `issuer` or IdP unreachable from the container; check network egress |
| "user not enabled: \<email\>" | the user does not exist or has a different `provider`; create it with `user --email … --provider <id>` |
| "anti-CSRF check failed" / "session expired" | `xtk_oidc` cookie lost (more than 10 min between start and callback, or cookies blocked) — start over from login |
| Microsoft: login ok but email empty | enable the optional `email` claim in the app, or rely on `preferred_username` |
| The button does not appear on `/login` | provider `enabled:false`, or you did not run `rebuild` after touching `config.json` |

Failed attempts (OIDC included) end up in `logs/auth.log` with
`event=oidc …` → integrable into **fail2ban** (see `INSTALL.md` §8).
