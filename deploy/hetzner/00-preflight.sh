#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# STAGE 0 — preflight. READ-ONLY and LOCAL only: verifies that every piece of
# information is correct BEFORE anything destructive runs. Safe to run anytime.
#
#   * validates the env file has all required variables
#   * hits the Hetzner API (read-only GET) to confirm the token works and the
#     server id/name/ip/location match what the env claims
#   * confirms the target OS image name exists (ubuntu-24.04 by default)
#   * ensures a local SSH keypair exists for the deploy (generates it in apply
#     mode; in dry-run only reports that it would)
#
# This stage runs its API GETs even in dry-run — that IS the verification.
set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

XTK_IMAGE="${XTK_IMAGE:-ubuntu-24.04}"

say "STAGE 0 — preflight (read-only)"; mode_banner
load_env
require_vars HCLOUD_TOKEN HCLOUD_SERVER_ID
# (addon-specific vars, e.g. private-repo credentials, are validated by the addon itself)

# --- 1. token + server exist (read-only) ------------------------------------
say "1/4 verify token + server ${HCLOUD_SERVER_ID} via API (read-only)"
srv="$(api_ro GET "/servers/${HCLOUD_SERVER_ID}")"
s_name="$(json_get "$srv" name)"
s_status="$(json_get "$srv" status)"
s_ip="$(printf '%s' "$srv" | grep -oE '"ip":[[:space:]]*"[0-9.]+"' | head -1 | sed -E 's/.*"([0-9.]+)"/\1/' || true)"
s_image="$(printf '%s' "$srv" | grep -oE '"name":[[:space:]]*"ubuntu[^"]*"' | head -1 | sed -E 's/.*"(ubuntu[^"]*)"/\1/' || true)"
[ -n "$s_name" ] || die "could not read server name from API response"
ok "server found: name=${s_name} status=${s_status} ip=${s_ip:-?} current_image=${s_image:-?}"

# --- 2. cross-check env against reality --------------------------------------
say "2/4 cross-check env vs API"
if [ -n "${HCLOUD_SERVER_NAME:-}" ] && [ "$HCLOUD_SERVER_NAME" != "$s_name" ]; then
	warn "HCLOUD_SERVER_NAME='${HCLOUD_SERVER_NAME}' but API says '${s_name}'"
else ok "server name matches"; fi
if [ -n "${HCLOUD_SERVER_IP:-}" ] && [ -n "$s_ip" ] && [ "$HCLOUD_SERVER_IP" != "$s_ip" ]; then
	warn "HCLOUD_SERVER_IP='${HCLOUD_SERVER_IP}' but API says '${s_ip}'"
else ok "server ip matches (${s_ip:-n/a})"; fi

# --- 3. target image exists --------------------------------------------------
say "3/4 confirm target image '${XTK_IMAGE}' exists"
imgs="$(api_ro GET "/images?type=system&per_page=100")"
if printf '%s' "$imgs" | grep -qE "\"name\": *\"${XTK_IMAGE}\""; then
	ok "image '${XTK_IMAGE}' available for rebuild"
else
	warn "image '${XTK_IMAGE}' not found in system images — check 'name' with: hcloud image list"
fi

# --- 4. local SSH key --------------------------------------------------------
say "4/4 local deploy SSH key (${XTK_SSH_KEY})"
if [ -f "$XTK_SSH_KEY" ]; then
	ok "key present: $(ssh-keygen -lf "${XTK_SSH_KEY}.pub" 2>/dev/null || echo 'pub missing!')"
else
	if dry; then warn "key absent — apply mode would generate: ssh-keygen -t ed25519 -f ${XTK_SSH_KEY}"
	else
		mkdir -p "$(dirname "$XTK_SSH_KEY")"; chmod 700 "$(dirname "$XTK_SSH_KEY")"
		ssh-keygen -t ed25519 -N '' -C "custode-deploy@hetzner-${HCLOUD_SERVER_ID}" -f "$XTK_SSH_KEY"
		chmod 600 "$XTK_SSH_KEY"; ok "generated $XTK_SSH_KEY"
	fi
fi

say "preflight complete — env and target verified"
