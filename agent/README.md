# xtk-agent — Xal-Tor-Ka hosting agent

A small **privileged host daemon** that runs a **fixed, vetted set of hardened
commands** on behalf of the internal (unprivileged) hosting extension, over a
**unix socket**. It is the ONLY component with host powers (create OS users, run
docker, manage databases); the internet-facing gateway never gains them.

## Why it is safe (non-injectable by construction)
- **Vetted scripts only.** Each command is a script in the commands dir, owned
  `root:root` (or a configured trusted uid) and **not group/world-writable**. The
  agent refuses any script that fails this check. Adding a capability = *placing a
  vetted script*; the extension can never add or alter a command (that needs root).
- **Allow-list parameters.** Each command has a manifest (`<name>.json`) declaring
  its parameters with a `pattern` (regexp, fully anchored) or `enum`. Unknown or
  non-matching params are rejected before anything runs.
- **No shell.** Validated params reach the script **only as environment**
  (`XTK_P_<NAME>`); the agent execs the script *by path* (never `sh -c`), so there
  is no command/argument injection surface. (Test: `TestRunPassesEnvNotShell`.)
- **Audited.** Every call is logged (peer uid/gid, command, param **keys** — never
  values, exit code) to the journal.
- **Restricted callers.** Socket mode `0660`; optionally group-owned
  (`--socket-gid`) and/or pinned to one peer uid (`--allow-uid`, SO_PEERCRED).

## Adding a command
1. Write `commands/<name>.sh` — the trust boundary. Read params from `XTK_P_*`.
   Map safe tokens to real resources *inside the script* (see `logtail.sh`).
2. Write `commands/<name>.json` — the manifest: description, `timeout_seconds`,
   and each param's `required` + `pattern`/`enum`.
3. Deploy both to `/usr/local/lib/xtk-agent/commands/` as `root:root`, `0755`.
   No agent recompile: it discovers commands at startup.

## Wire protocol (one request per connection, JSON)
Request:  `{"cmd":"logtail","params":{"log":"nginx-error","lines":"100"}}`
Response: `{"ok":true,"code":0,"stdout":"…","stderr":""}`  (or `{"error":"…"}`)

## Commands shipped
- `sysinfo` — read-only host/docker summary (no params).
- `logtail` — tail a **whitelisted** log (`enum` + `pattern` params); the
  log-analysis pattern.
- `site_create` — provision a site: per-site OS user in `docker-hosting`, site dir
  under `/opt/sites/<name>`, and a rendered compose (from `templates/<template>`).
  Does not start it. Prints `upstream=http://<name>.site` for the gateway.
- `site_up` / `site_down` / `site_status` — `docker compose` lifecycle of a site.
- `site_destroy` — stop and remove a site (`compose down -v`, remove the dir,
  `userdel` the OS user). Refuses if the site does not exist.
- `db_create` — create a database + dedicated user on the **shared** engine
  instance (`engine` ∈ `{pg, mysql}`), bringing that instance up on first use
  under `/opt/xtk-db/<engine>/`. Refuses if the db already exists. Prints the
  connection (`host db user password …`) on stdout for the caller to inject; the
  generated password is never audited (only param keys are).

Site containers run as the site's uid:gid and join the external `xtk-hosting`
network (alias `<name>.site`); the gateway reverse-proxies there. No host port is
published. Templates live in `templates/` (first: `php-fpm` = nginx + php-fpm).
Shared DB instances get the alias `xtk-db-<engine>.db` on the same network.

## Deploy
    go build -ldflags="-s -w" -o xtk-agent ./agent/xtk-agent
    install -m0755 xtk-agent /usr/local/bin/
    install -d -m0755 /usr/local/lib/xtk-agent/commands
    install -m0755 agent/commands/*.sh   /usr/local/lib/xtk-agent/commands/
    install -m0644 agent/commands/*.json /usr/local/lib/xtk-agent/commands/
    install -m0644 deploy/agent/xtk-agent.service /etc/systemd/system/
    systemctl enable --now xtk-agent
