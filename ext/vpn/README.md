# VPN extension — `xtk-vpn-ui`

A node-agnostic VPN module for XalTorKa (WireGuard v1, driver-abstracted), the third
tier of the pattern (agent → extension → core). An internal web UI that, from F2 on,
manages the WireGuard data-plane by driving the privileged **xtk-vpn-agent** over its
unix socket. It has **no host powers of its own**: every mutating action is a vetted
agent command. Twin of `ext/hosting` — see `../../DRAFT-ext-module.md` for the reusable
module pattern and `DESIGN.md` for the full spec (rev 0.5).

```
browser ─► gateway (nginx) ─► core (Go, admin session gate) ─proxy─► xtk-vpn-ui
                                                                          │ unix socket
                                                                          ▼
                                                                   xtk-vpn-agent (root, vetted)
                                                                          │ vetted cmds
                                                                          ▼
                                                            xtk-vpn data-plane (NET_ADMIN, wg0)
```

## Node-agnostic
The binary carries **no node identity**: listen address, socket, and config path are
flags with generic defaults; the node-specific values (public endpoint, subnets, keys)
live in `vpn.json` and the installer's env, never hardcoded. The core reaches the module
by its docker service name (`http://xtk-vpn-ui:8091`), valid on any node — the module
installs the same way on hydra's node or on `xaltorka1.hosting4agency.com`.

## How it integrates with the core (no nginx changes)
- The core reverse-proxies `/admin/vpn/*` to this service, **gated by the admin session**
  (`adminSessionOK`: admin IP whitelist + valid 2FA session + admin user).
- Enabled by `VPN_UPSTREAM` on the core (set by the compose overlay). When empty,
  `/admin/vpn` 404s and the nav entry is hidden — plain installs are unaffected.
- The module's own config is `vpn.json` (owned by the extension, hot-reloadable), kept
  **out** of the core `config.json` (which is strict-decode). The core knows only the
  upstream exists.

## Config — `vpn.json`
Static (`driver`, `role`, `listen_port`, `vpn_subnet`, `docker_net`) + the access matrix
(`vpn_machines[]`, `vpn_users[].can_reach`). See `vpn.json.example`. `enabled=false` (the
default) makes the module inert: the UI shows only status, no data-plane, no network effect.

## Phases
- **F1 (this scaffold):** module + driver abstraction + core wiring; inert. Status page only.
- **F2:** data-plane container `xtk-vpn` + vetted `xtk-vpn-agent` + real WireGuard driver
  (peer keys generated on-machine, never transmitted).
- **F3:** hub + reach A (route) / B (tunnel) + the access matrix (nftables forward-filter).
- **F4:** failsafe re-entry ladder + anti-lockout invariant. **F5:** behind-hub hardening +
  active defenses. **F6:** full `/admin/vpn` panel.

## Run (F1)
```
docker compose -f docker-compose.yml -f ext/vpn/docker-compose.yml up -d --build
# or, node-agnostic and sticky:  sudo deploy/vpn/install.sh --overlay
```

## Pages
- `GET /admin/vpn` — module status (driver, role, port, subnet, machine/user counts).
- (F2+) machines, users, matrix, onboarding, re-entry.
