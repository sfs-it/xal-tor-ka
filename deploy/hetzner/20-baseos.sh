#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# STAGE 2 — base OS. Prepares the freshly-rebuilt Ubuntu Server: system update,
# Docker Engine + compose plugin, git, and a restrictive host firewall (ufw).
# All commands run remotely over SSH; dry-run prints them.
#
# Firewall policy for a TEST node (not public yet): allow 22/80/443, deny the
# rest. Tighten to admin-CIDR-only on 22 when the node graduates to production.
set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

say "STAGE 2 — base OS on ${HCLOUD_SERVER_IP:-<ip>}"; mode_banner
load_env
require_vars HCLOUD_SERVER_IP

say "1/4 apt update + upgrade"
ssh_run "export DEBIAN_FRONTEND=noninteractive; apt-get update -qq && apt-get upgrade -y -qq"

say "2/4 install Docker Engine + compose plugin + git + make + ufw"
ssh_run "command -v docker >/dev/null 2>&1 || curl -fsSL https://get.docker.com | sh"
# make is needed by stages 4-5 (make up / make hosting-install); the Go build
# itself runs inside Docker, so no host toolchain beyond make is required.
ssh_run "export DEBIAN_FRONTEND=noninteractive; apt-get install -y -qq git make ufw ca-certificates"
ssh_run "systemctl enable --now docker"

say "3/4 firewall: allow 22/80/443, default deny incoming"
ssh_run "ufw --force reset >/dev/null && ufw default deny incoming && ufw default allow outgoing && ufw allow 22/tcp && ufw allow 80/tcp && ufw allow 443/tcp && ufw --force enable"

say "4/4 verify (assert, do not trust)"
ssh_run "docker --version && docker compose version && git --version && ufw status verbose"

say "base OS complete"
