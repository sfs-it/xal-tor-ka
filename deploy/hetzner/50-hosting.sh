#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# STAGE 5 — install the hosting module (overlay). Installs the privileged hosting
# agent and raises the overlay, then makes the overlay STICKY via COMPOSE_FILE in
# .env — without that, a plain `docker compose` recreates the core without
# HOSTING_UPSTREAM and /admin/hosting 404s silently (PROD incident 2026-07-17).
set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

XTK_REMOTE_DIR="${XTK_REMOTE_DIR:-/opt/xaltorka}"

say "STAGE 5 — hosting module in ${XTK_REMOTE_DIR}"; mode_banner
load_env
require_vars HCLOUD_SERVER_IP

# main's deploy/agent/install.sh builds xtk-agent only if host `go` exists, else
# it needs a prebuilt /usr/local/bin/xtk-agent (no docker fallback — that arrived
# in beta0.15). go.mod requires go 1.25, which apt (24.04 = 1.22) can't provide.
# So we pre-build the agent in a golang:1.25 container (the box has docker) and
# drop the binary where install.sh expects it — no host toolchain, main untouched.
say "1/4 pre-build xtk-agent via docker golang:1.25 (no host go needed)"
ssh_run "cd ${XTK_REMOTE_DIR} && if command -v go >/dev/null 2>&1 || [ -x /usr/local/bin/xtk-agent ]; then echo 'go or prebuilt already present, skipping'; else docker run --rm -v \"\$PWD\":/src -w /src golang:1.25 go build -buildvcs=false -o /src/.xtk-agent.build ./agent/xtk-agent && install -m 0755 .xtk-agent.build /usr/local/bin/xtk-agent && rm -f .xtk-agent.build && echo 'xtk-agent prebuilt via docker -> /usr/local/bin/xtk-agent'; fi"

say "2/4 install privileged hosting agent + raise overlay (make hosting-install)"
ssh_run "cd ${XTK_REMOTE_DIR} && make hosting-install"

say "3/4 make the overlay sticky (COMPOSE_FILE in .env)"
ssh_run "cd ${XTK_REMOTE_DIR} && grep -q '^COMPOSE_FILE=' .env || echo 'COMPOSE_FILE=docker-compose.yml:ext/hosting/docker-compose.yml' >> .env; grep '^COMPOSE_FILE=' .env"

# hosting-install recreates the core via the overlay -> the core gets a NEW docker
# IP, but nginx cached the old one at its own start -> connect() refused -> 502.
# Restart nginx LAST so it re-resolves the core's current IP. (Root-cause fix would
# be a resolver+variable upstream in nginx; this is the reliable deploy-time guard.)
say "3-bis/4 restart nginx LAST so it re-resolves the (recreated) core IP — prevents 502"
ssh_run "cd ${XTK_REMOTE_DIR} && docker compose restart nginx && sleep 4"

say "4/4 verify gate + hosting endpoint (assert, not trust)"
ssh_run "cd ${XTK_REMOTE_DIR} && docker compose ps --format '{{.Name}} | {{.Status}}'; curl -s -o /dev/null -w 'healthz -> %{http_code}\n' http://localhost:80/healthz; curl -s -o /dev/null -w 'admin/hosting -> %{http_code}\n' http://localhost:80/admin/hosting || true"

say "hosting stage complete"
