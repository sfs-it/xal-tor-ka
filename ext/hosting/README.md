# Hosting extension — `xtk-hosting-ui`

The third tier of the hosting platform (agent → extension → core). An internal web
UI that manages lightweight Docker sites by driving the privileged **xtk-agent**
over its unix socket. It has **no host powers of its own**: every mutating action is
a vetted agent command (see `../../agent/`).

```
browser ─► gateway (nginx) ─► core (Go, admin session gate) ─proxy─► xtk-hosting-ui
                                                                          │ unix socket
                                                                          ▼
                                                                     xtk-agent (root)
                                                                          │ vetted cmds
                                                                          ▼
                                                              site containers on xtk-hosting
```

## How it integrates with the core (no nginx changes)
- The core reverse-proxies `/admin/hosting/*` to this service, **gated by the admin
  session** (`adminSessionOK`: admin IP whitelist + valid 2FA session + admin user).
  Auth stays centralized in the core; the extension trusts the gateway.
- Enabled by `HOSTING_UPSTREAM` on the core (set by the compose overlay). When empty,
  `/admin/hosting` 404s and the nav entry is hidden — plain installs are unaffected.
- Shared look via `xtkui.Chrome` (assets served by the core at `/assets/`).

## Run
```
# host: the agent daemon + a socket group the UI can reach
groupadd -g 1997 xtk-agent
# add ' --socket-gid 1997' to ExecStart in the xtk-agent systemd unit; restart it
docker compose -f docker-compose.yml -f ext/hosting/docker-compose.yml up -d --build
```

## Pages
- `GET /admin/hosting` — list sites (`site_list`), with up/down/destroy actions.
- `POST /admin/hosting/create` — `site_create` + `site_up` (name + template).
- `POST /admin/hosting/{up,down,destroy}` — single-site lifecycle.

## Still to do
- Localize the page copy (hosting i18n keys across the 10 locales); the shared
  chrome is already localized.
- DB actions (`db_create`) surfaced in the UI; per-site backend publish shortcut.
