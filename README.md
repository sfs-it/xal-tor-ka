# ⛬ Xal-Tor-Ka · `beta0.1`

> Authentication gatekeeper & reverse-proxy manager in Docker. NGINX is the only
> exposed surface; an internal Go service answers every request via `auth_request`
> and decides **pass / login / deny** from a per-backend authorization matrix.

**Xal-Tor-Ka** sits in front of a public VPS. NGINX is the single exposed entry
point (a static shield); an internal Go service — never exposed directly —
evaluates every request through NGINX's `auth_request` and returns **200 / 401 /
403** based on a per-host, per-path matrix with three levels: `public`,
`authenticated` (TOTP 2FA), and `whitelist` (explicitly allowed users).
Authentication is **local** (argon2id + TOTP) or delegated to an **OIDC provider**
(Google, Microsoft/Entra, Keycloak, Authentik, Auth0, Okta…). The whole
configuration lives in JSON and is managed from a hardened, IP-restricted web admin
— with snapshot backups, hot reload, health checks and a fail2ban-friendly audit
log.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200=pass / 401=login / 403=deny
                  └── proxy_pass (only if authorized) ──► internal backends
```

## Status

**`beta0.1`** — pre-1.0. Core is implemented and runs (local auth + TOTP, OIDC
login, reverse-proxy manager, admin UI, health checks, backups, fail2ban log).
Open items tracked in [`TODO.md`](TODO.md).

## Docs

- [`INSTALL.md`](INSTALL.md) — deploy on a remote VPS, step by step.
- [`AUTH-PROVIDERS.md`](AUTH-PROVIDERS.md) — enable Google / Microsoft / generic OIDC.
- [`BLUEPRINT.md`](BLUEPRINT.md) — authoritative architecture & data model.

## Quick start (local)

```bash
make up                 # build + start the stack (NGINX exposed on :80)
make setup EMAIL=you@example.com   # one-time admin onboarding (token URL)
make version            # -> beta0.1
```

## Naming

**`Xal-Tor-Ka`** when you talk *about* it (brand, UI, docs); **`xaltorka`** when you
talk *to* it (Go module, binary, Docker service, hostname, logs).

## Versioning

Single source of truth: [`version/version.go`](version/version.go). Release builds
override it at link time: `-ldflags "-X xaltorka/version.Version=beta0.2"`. Pre-1.0
line is `beta0.N`. Surfaced in `xaltorka version`, `/healthz`, the startup log, the
admin topbar and the login footer.

---

© 2026 **SFS.it di Zanutto Agostino** — licensed under the [Apache License 2.0](LICENSE).
