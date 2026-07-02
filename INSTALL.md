# Xal-Tor-Ka — Installation on a remote machine

A simple guide to deploying the gateway to production on a VPS (Debian/Ubuntu).
NGINX is the only exposed service; the Go service stays internal.

> **Prerequisites on the VPS:** Docker Engine + Compose plugin
> (`docker --version`, `docker compose version`). On Debian/Ubuntu:
> `curl -fsSL https://get.docker.com | sh`.

---

## 1. Copy the files to the VPS

You need a folder (e.g. `/opt/xaltorka`) containing:

```
/opt/xaltorka/
├── docker-compose.yml
├── Dockerfile
├── .env                 # created by you (see step 2)
├── config.json
├── secrets.json         # created by you from the .example
├── users.json           # created by you (empty at first)
├── services.json
├── go.mod  go.sum  main.go  *.go  ...   # the source (needed for the build)
└── nginx/conf.d/xaltorka.conf
```

Simpler: clone/copy the entire repository into the folder.

```bash
sudo mkdir -p /opt/xaltorka && sudo chown "$USER":"$USER" /opt/xaltorka
# copy the project files here (git clone, scp, rsync...)
cd /opt/xaltorka
cp secrets.example.json secrets.json
printf '{ "users": [] }\n' > users.json
```

## 2. Configure the environment (`.env`)

```bash
cp .env.example .env
nano .env
```

Set at least:

| Variable     | What to put |
|--------------|---------------|
| `GATE_URL`   | public URL, e.g. `https://gate.yourdomain.com` |
| `HTTP_PORT`  | NGINX public port (default `80`) |
| `TLS_MODE`   | `external` if you have a reverse proxy/LB with TLS in front; otherwise `selfsigned` |
| `ADMIN_CIDR` | YOUR IP (or VPN) allowed to use the admin area, e.g. `203.0.113.7/32` |
| `EDGE_CIDR`  | the network the `X-Forwarded-*` headers come from (docker subnet or upstream proxy) |
| `PUID`/`PGID`| output of `id -u` / `id -g` (owner of the config files) |

> **TLS:** in production the recommendation is to terminate TLS upstream (host
> reverse proxy or LB) and leave `TLS_MODE=external`. The `acme` mode (Let's Encrypt
> via PowerDNS) is planned but not yet implemented (BLUEPRINT §3.1).

## 3. Start the stack

```bash
make up        # = docker compose up -d --build
make ps        # service status
make logs      # live logs
```

Verify:

```bash
curl -s http://localhost:${HTTP_PORT:-80}/healthz   # -> {"status":"ok",...}
```

## 4. Create the first administrator (hybrid onboarding)

Setup starts from the CLI and finishes in the browser (no manual file editing).

```bash
make setup EMAIL=admin@yourdomain.com
# or:
docker compose run --rm xaltorka setup --email admin@yourdomain.com --config /etc/xaltorka
```

The command prints a URL with an expiring token, e.g.:

```
http://localhost/setup?token=XXXXXXXX
```

Open it in the browser (replacing the host with `GATE_URL` if you connect remotely):

1. the page shows the email already filled in → set the **password**;
2. a **QR** appears: scan it with an authenticator app (Google Authenticator,
   Authy, …) or enter the key manually;
3. type the **6-digit code** to confirm → profile activated.

From now on: log in at `${GATE_URL}/login`.

## 5. Add services

Two types of "service" show up in the `/listing` dashboard:

**a) Reverse-proxied backends** (routed by the gateway):

```bash
docker compose run --rm xaltorka add-backend \
  --config /etc/xaltorka \
  --id intranet --name "Intranet" \
  --host intranet.yourdomain.com \
  --upstream http://10.0.0.10:8080 \
  --rule whitelist
# apply: restart the service
make rebuild
```

**b) External links** (just a tile/bookmark in the dashboard, not proxied):

```bash
docker compose run --rm xaltorka add-link \
  --config /etc/xaltorka \
  --id wiki --name "Wiki" --url https://wiki.yourdomain.com --public
# apply hot (from YOUR IP in ADMIN_CIDR):
curl -X POST http://localhost/admin/reload
```

**c) Automatic discovery of Docker containers** (convenience, optional):
from `/admin` → «Discover containers» you see containers with published ports and with one
click you create the vhost `<name>.localhost → host.docker.internal:<port>`. Visibility
goes through a **read-only** `docker-socket-proxy` sidecar (docker.sock is NOT mounted
in Xal-Tor-Ka). To disable it: remove the `docker-socket-proxy` service from the compose file
and the `DOCKER_PROXY` variable. On production Linux it needs `host-gateway`
support (already set via `extra_hosts`).

> Authorizations: a `public` service (link) or a `public`/`authenticated` backend
> is visible to all logged-in users. For `whitelist` ones, add the service `id`
> to the user's `backends` list in `users.json` (user management from
> `/admin` coming soon).

## 5-ter. 2FA (TOTP)

The TOTP second factor is enabled by default. To disable it (password only), set
`"disable_totp": true` in `config.json` and restart. **In production keep it `false`.**

## 5-quater. Login with external providers (Google/Microsoft/OIDC)

Besides local login, you can enable access via **Google**, **Microsoft (Entra
ID)** or any **OIDC** IdP (Keycloak, Authentik, Auth0, Okta, GitLab). The
providers are already present in `config.json` as **disabled** examples; to
enable them (app registration, `client_id`/`client_secret`, redirect URI, user
creation) follow the dedicated guide **[`AUTH-PROVIDERS.md`](AUTH-PROVIDERS.md)**.

> After touching `config.json` you need **`make rebuild`** (not a plain
> restart): the binary re-reads the schema at startup and the provider field is new.

## 5-bis. Administration (unified model)

There is no separate admin password: **the administrator is a user**
(`users.json`) with the `admin: true` flag. You log in with the normal login (`/login`);
admins also see `/admin` (on top of the `ADMIN_CIDR` IP whitelist, network level).

- The **first admin** is the profile created by the initial setup (step 4).
- Create/promote an admin from the CLI (recovery/bootstrap):
  `make admin EMAIL=you@domain PASSWORD=secret`
  (equivalent to `docker compose run --rm xaltorka user --email … --password … --admin`).
- From `/admin → Users` you can grant/revoke the admin flag, reset passwords and 2FA.

## 6. Operations

| Command        | Effect |
|----------------|---------|
| `make up`      | build + start in the background |
| `make down`    | stop and remove containers |
| `make logs`    | live logs |
| `make rebuild` | rebuild images + restart |
| `make ps`      | service status |

**Backup:** on every write of `users.json`/`services.json` a snapshot is created
in `backups/` (timestamped). Secrets (`secrets.json`, `users.json`) must never
be versioned: they are already in `.gitignore`.

## 7. Security notes

- The Go service **exposes no ports**: it is reachable only from the internal docker network.
- `secrets.json` and `users.json` contain secrets → `600` permissions, no git.
- The `/admin` area is restricted to `ADMIN_CIDR`. Behind NGINX, the real client IP
  arrives via `X-Forwarded-For` and is trusted only from the networks in
  `EDGE_CIDR`.
- In production, put the gateway behind HTTPS (`TLS_MODE=external` + TLS termination
  upstream) so session cookies travel as `Secure`.

## 8. fail2ban (brute-force protection)

Xal-Tor-Ka writes failed access attempts (login, 2FA, admin access) to
`logs/auth.log`, with the real client IP (via `X-Forwarded-For` from the
`trusted_proxies`). Per-line format:

```
2026-06-24T10:00:00Z xaltorka auth-fail ip=<IP> event=<login|totp|admin_denied|admin_ip> ...
```

To enable banning on the host (fail2ban already installed):

```bash
sudo cp fail2ban/filter.d/xaltorka.conf /etc/fail2ban/filter.d/xaltorka.conf
sudo cp fail2ban/jail.d/xaltorka.local /etc/fail2ban/jail.d/xaltorka.local
# adjust logpath in jail.d/xaltorka.local to the real path of logs/auth.log
sudo systemctl restart fail2ban
sudo fail2ban-client status xaltorka
```

The log path is configurable with `auth_log` in `config.json` (default `logs/auth.log`).

## 9. Deploy without Docker (host / LXD / dedicated machine)

> ⚠️ **Beta scaffolding, not yet tested end-to-end on a real machine.**
> The *core* (Go service) is a static binary already agnostic to the runtime; the
> Docker-specific points are governed by three **knobs** via env (default = Docker):
>
> | Env | Docker (default) | Host/LXD |
> |-----|------------------|----------|
> | `DEPLOY_MODE` | `docker` | `host` |
> | `NGINX_RELOAD_CMD` | *(empty: reload handled by the nginx container)* | `nginx -s reload` / `systemctl reload nginx` |
> | `UPSTREAM_LOCALHOST` | `host.docker.internal` | `127.0.0.1` |
>
> `DEPLOY_MODE=host` sets the right-hand column defaults on its own; the other two
> env vars override it when set.

Steps (Debian/Ubuntu or inside an LXD container with NGINX installed):

```bash
# 1) build the static binary (on the build machine) and copy it to the target
make build                      # -> ./xaltorka (CGO off, static)
sudo install -m 0755 xaltorka /usr/local/bin/xaltorka
xaltorka version                # -> beta0.2

# 2) create a service user and a configuration folder
sudo useradd --system --home /opt/xaltorka --shell /usr/sbin/nologin xaltorka
sudo mkdir -p /opt/xaltorka && sudo chown xaltorka:xaltorka /opt/xaltorka
# copy config.json here, and create secrets.json/users.json from the .example
sudo -u xaltorka cp secrets.example.json /opt/xaltorka/secrets.json
sudo -u xaltorka sh -c 'printf "{ \"users\": [] }\n" > /opt/xaltorka/users.json'

# 3) host NGINX: include the vhosts generated by the Go service
#    (the service writes /opt/xaltorka/nginx/conf.d/backends.conf)
echo 'include /opt/xaltorka/nginx/conf.d/*.conf;' | sudo tee /etc/nginx/conf.d/xaltorka-include.conf

# 4) reload NGINX without a password for the service user
sudo install -m 0440 deploy/xaltorka.sudoers /etc/sudoers.d/xaltorka

# 5) systemd service
sudo cp deploy/xaltorka.service /etc/systemd/system/xaltorka.service
sudoedit /etc/systemd/system/xaltorka.service   # adjust GATE_URL and paths
sudo systemctl daemon-reload
sudo systemctl enable --now xaltorka
journalctl -u xaltorka -f

# 6) onboarding of the first admin (as in §4, but from the local CLI)
sudo -u xaltorka xaltorka setup --email admin@yourdomain.com --config /opt/xaltorka
```

**Points to verify in the field (beta):**
- **Gate upstream** in the generated vhosts: set `PROXY_UPSTREAM=127.0.0.1:8080`
  (already in the unit) and have the Go service listen on `127.0.0.1:8080`
  (`server.listen` in `config.json`).
- **NGINX `resolver`**: generation uses `PROXY_RESOLVER` (default `127.0.0.11`,
  Docker's internal DNS). On a host, set it to the system resolver
  (e.g. `127.0.0.53` with systemd-resolved) or adapt the vhosts.
- **Reload permissions**: the `sudoers` above covers `nginx -s reload`; if you use
  `systemctl reload nginx`, update `NGINX_RELOAD_CMD` and the sudoers rule.
- **TLS**: as in Docker, upstream termination is assumed (`TLS_MODE=external`).
