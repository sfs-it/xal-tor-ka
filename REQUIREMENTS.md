# Xal-Tor-Ka — Requirements

What you need to run Xal-Tor-Ka. Two deployment paths are supported; pick one. For
the actual procedure see [`INSTALL.md`](INSTALL.md).

## 1. Choose a deployment path

| Path | Best for | You provide |
|------|----------|-------------|
| **Docker** (recommended) | most servers, quickest setup | Docker Engine + Compose plugin |
| **Host / LXD / dedicated** | no-Docker policy, system integration | Go toolchain (or a prebuilt binary), system NGINX, systemd |

## 2. Docker path

- **Docker Engine** ≥ 24 and the **Compose plugin** (`docker --version`,
  `docker compose version`). Install on Debian/Ubuntu: `curl -fsSL https://get.docker.com | sh`.
- A **Linux host** (Debian/Ubuntu recommended) for production. For development,
  **Windows 11 + WSL2 + Docker Desktop** is supported.
- The build is multi-stage: the Go toolchain is **not** needed on the host (it runs
  inside the build container). You only need the project sources + Docker.
- **Resources**: small — the Go service is capped at ~256 MB; NGINX is lightweight.
  A 1 vCPU / 1 GB VPS is enough to start.

## 3. Host / LXD / dedicated path

- **NGINX** installed as a system service.
- A way to build the static binary (**Go ≥ 1.25** on a build machine) **or** a
  prebuilt `xaltorka` binary copied to the target. The binary is static
  (`CGO_ENABLED=0`), so the runtime host needs no Go and no libc.
- **systemd** to run the service (sample unit in [`deploy/`](deploy/)).
- Permission for the service user to reload NGINX (a `sudoers` drop-in is provided).
- See the three deployment knobs (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`,
  `UPSTREAM_LOCALHOST`) in [`INSTALL.md`](INSTALL.md) §9.

> Status: the non-Docker path is provided and wired but **not yet field-tested** on
> a real machine (beta).

## 4. Network and DNS

- A **public hostname** for the gateway (e.g. `gate.yourdomain.tld`) and DNS records
  for each backend subdomain you expose, all pointing at the server.
- **Public ports**: only NGINX is exposed (HTTP `80` and/or HTTPS `443`). The Go
  service and internal backends are **not** published.
- **TLS**: termination is expected upstream (`TLS_MODE=external` behind a host
  reverse proxy or load balancer). HTTPS is required in production so session
  cookies travel as `Secure`. (Self-signed auto-generation is planned, not yet
  implemented.)

## 5. Configuration files

Created from the provided templates (`*.example.json`):

- `config.json` — infrastructure config (env-templated; safe to version).
- `secrets.json` — secrets; **never** versioned (perms `600`).
- `users.json` — users/authorizations; **never** versioned (perms `600`).
- `services.json` — runtime-managed services (optional; created/edited via the admin
  UI or CLI).

## 6. Optional integrations

- **OIDC login**: an account/app registration with **Google**, **Microsoft/Entra**,
  or any OIDC provider (Keycloak, Authentik, Auth0, Okta, GitLab). See
  [`AUTH-PROVIDERS.md`](AUTH-PROVIDERS.md).
- **Brute-force protection**: **fail2ban** on the host (filter + jail samples
  provided).
- **Alerting**: an **SMTP** account and/or a **Telegram** bot for health alerts
  (optional; not required to run).

## 7. Client side

- Any modern browser for the admin panel and login.
- An **authenticator app** (Google Authenticator, Authy, …) for users with TOTP
  two-factor enabled.
