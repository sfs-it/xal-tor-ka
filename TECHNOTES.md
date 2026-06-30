# Xal-Tor-Ka — Technical notes (what it does and how)

A technical but readable explanation of how it works. For the **rigorous
specification** (JSON schema, data models, endpoint contract) see
[`BLUEPRINT.md`](BLUEPRINT.md); to install it, see [`REQUIREMENTS.md`](REQUIREMENTS.md)
+ [`INSTALL.md`](INSTALL.md).

## In one sentence

Xal-Tor-Ka is an **authentication gatekeeper + reverse-proxy manager**: it puts
**NGINX** as the only internet-exposed service, and for every request it asks an
**internal Go service** (never exposed) whether to pass, ask for login, or deny.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## How it decides: the `auth_request` flow

NGINX **does not know the rules**. For every incoming request it makes an internal
*subrequest* to the Go service's `/validate` endpoint, passing the original host and
path. The service replies with an HTTP status:

- **200** → NGINX proceeds with `proxy_pass` to the real backend;
- **401** → NGINX redirects to the login page;
- **403** → access denied.

The principle is **fail-closed**: any error, timeout or doubt during evaluation
yields 401/403, **never** a 200. The Go service is not reachable from outside: only
NGINX, on the internal network, can query it.

## The core: the authorization matrix

For each **host** and **path**, one of three rules applies:

| Rule | Who gets in |
|------|-------------|
| `public` | anyone |
| `authenticated` | users with a valid session and **2FA** (TOTP) completed |
| `whitelist` | only users **explicitly authorized** for that service |

**Administrators** can access everything. Granularity is per subdomain and per path
(e.g. `/` public but `/admin` whitelist on the same host).

## Authentication

- **Local**: passwords hashed with **argon2id** + a second factor via **TOTP**
  (RFC 6238, compatible with Google Authenticator/Authy).
- **OIDC** (OpenID Connect): login delegated to **Google**, **Microsoft/Entra** or
  generic providers (**Keycloak, Authentik, Auth0, Okta, GitLab**). The identity
  token's signature is verified against the provider's public keys.
  - **No auto-provisioning**: the user must already exist, declared for that
    provider — signing in with Google is not enough to get in. See
    [`AUTH-PROVIDERS.md`](AUTH-PROVIDERS.md).
- **Sessions**: `HttpOnly`/`SameSite=Lax` cookies (and `Secure` behind HTTPS),
  kept in RAM with file persistence (they survive a restart).

## Configuration: everything in JSON

No mandatory database: configuration lives in a few typed JSON files, validated at
startup (**Fail-Fast**: an unknown field or out-of-range value blocks startup with a
clear message).

| File | Content |
|------|---------|
| `config.json` | infrastructure (env-templated): auth mode, TLS, sessions, admin IPs, providers |
| `secrets.json` | secrets (OIDC client secrets, tokens, SMTP) — never versioned |
| `users.json` | users, roles, 2FA, authorizations — never versioned |
| `services.json` | runtime-managed services (proxied backends + dashboard links) |

Changes to users/services apply **hot** (hot reload), without a restart.

## Components (Go service)

Stdlib-first, static binary. Main packages:

- `handlers/` — HTTP endpoints: `/validate`, login + TOTP, OIDC callback, setup,
  the `/admin` panel, the `/listing` dashboard.
- `providers/` — authentication: `local` and `oidc` (common interface).
- `matrix/` — evaluation of authorization rules (per host/path).
- `proxy/` — generates the NGINX backend vhosts and reloads.
- `health/` — periodic backend health checks (`/health` endpoint).
- `config/` — load + validation + atomic save with snapshots.
- `audit/` — log of failed access attempts (for fail2ban).
- `auth/` — hashing, TOTP, sessions, user directory.

## Reverse proxy: generation and reload

The manager generates the NGINX backend configuration (one `server{}` per host,
with `auth_request` on protected routes and `proxy_pass` to the upstream). The
**reload**:

- **Docker**: the NGINX container detects the change and reloads itself (polling),
  because `inotify` is unreliable on Docker Desktop/WSL2 bind mounts.
- **Host/LXD**: the Go service runs a configurable reload command
  (`nginx -s reload` / `systemctl reload nginx`).

NGINX always validates the new configuration and, if it is invalid, keeps the
running one: a bad regeneration does not take the proxy down.

## Management and operations

- **`/admin` panel** (IP-restricted): manage services, users and permissions,
  monitor status, in separate pages.
- **`/listing` dashboard**: shows each user only the services they can access.
- **Onboarding**: the first run generates an expiring token to create the first
  administrator via the web; then the interface locks down.
- **Backups**: every save creates a snapshot with auto-trash (keeps the last N),
  with restore also from the CLI.
- **Brute-force defense**: failed attempts land in a structured log
  (`logs/auth.log`) with the real client IP, pluggable into **fail2ban**.

## Security at a glance

- The only exposed service is **NGINX**; the Go service is internal.
- **Fail-closed** across the whole authorization path.
- Secret comparisons in **constant time**; secrets **never** logged.
- Admin area restricted by **IP**; the real client IP is taken from
  `X-Forwarded-For` only from trusted proxies.
- The setup token is **single-use** and expiring.

## Deployment

- **Docker Compose** (default): NGINX exposed, Go service internal, a read-only
  sidecar for container discovery, resource limits and log rotation.
- **Host / LXD / dedicated machine**: static binary + system NGINX, governed by
  three variables (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`).
  See [`INSTALL.md`](INSTALL.md) §9.

## Version

Single source of truth in `version/version.go` (pre-1.0 line `beta0.N`),
overridable at build time, and shown in `xaltorka version`, `/healthz`, the startup
log and the UI.
