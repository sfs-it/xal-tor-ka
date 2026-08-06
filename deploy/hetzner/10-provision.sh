#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# STAGE 1 — provision (DESTRUCTIVE). Rebuilds the existing server with a clean
# Ubuntu image and makes our deploy key work on it. The rebuild WIPES THE DISK.
# Runs only in apply mode; dry-run prints every call it would make.
#
# ⚠️ WHY THE RESCUE DANCE (a paid lesson, 2026-08-05):
# Hetzner `rebuild` re-applies only the SSH keys bound at server CREATE — a
# key registered afterwards never lands on the disk, so SSH gives
# "Permission denied (publickey)". And Hetzner's Ubuntu images ship with
# PasswordAuthentication=no, so reset_password is CONSOLE-ONLY (useless over SSH).
# The robust fix that preserves server id / IP / type (needed: cx22 is deprecated
# and cannot be recreated): rebuild -> enable_rescue WITH our key -> mount the
# installed root from rescue -> inject our pubkey -> disable_rescue -> reboot.
#
#   1. register the public SSH key (idempotent; needed for enable_rescue)
#   2. POST /servers/{id}/actions/rebuild  image=ubuntu-24.04   (WIPES DISK)
#   3. poll rebuild + wait running
#   4. inject our key into the installed system via rescue, verify key login
set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

XTK_IMAGE="${XTK_IMAGE:-ubuntu-24.04}"
KEY_NAME="${HCLOUD_SSH_KEY_NAME:-custode-deploy}"

say "STAGE 1 — provision (rebuild ${HCLOUD_SERVER_ID:-?} -> ${XTK_IMAGE})"; mode_banner
load_env
require_vars HCLOUD_TOKEN HCLOUD_SERVER_ID
if [ ! -f "${XTK_SSH_KEY}.pub" ]; then
	dry && warn "public key ${XTK_SSH_KEY}.pub missing — preflight --apply would create it (continuing dry-run)" \
	    || die "public key ${XTK_SSH_KEY}.pub missing — run 00-preflight.sh --apply first"
fi

# --- 1. register SSH key (idempotent) ---------------------------------------
say "1/4 ensure SSH key '${KEY_NAME}' is registered"
keys="$(api_ro GET "/ssh_keys?name=${KEY_NAME}")"
if printf '%s' "$keys" | grep -qE "\"name\": *\"${KEY_NAME}\""; then
	ok "key '${KEY_NAME}' already registered"
else
	if [ -f "${XTK_SSH_KEY}.pub" ]; then pub="$(cat "${XTK_SSH_KEY}.pub")"; else pub="ssh-ed25519 <generated-at-apply>"; fi
	body="$(printf '{"name":"%s","public_key":"%s"}' "$KEY_NAME" "$pub")"
	api_w POST "/ssh_keys" "$body" >/dev/null
	ok "key '${KEY_NAME}' registered"
fi

# --- 2. rebuild (DESTRUCTIVE) ------------------------------------------------
say "2/4 REBUILD server ${HCLOUD_SERVER_ID} with ${XTK_IMAGE}  (WIPES DISK)"
resp="$(api_w POST "/servers/${HCLOUD_SERVER_ID}/actions/rebuild" \
	"$(printf '{"image":"%s"}' "$XTK_IMAGE")")"
if dry; then
	say "dry-run: would then wait running + inject key via rescue (see lib.sh inject_key_via_rescue)"
	inject_key_via_rescue
	exit 0
fi
action_id="$(json_get "$resp" id)"
[ -n "$action_id" ] || die "no action id in rebuild response"
poll_action "$action_id" "rebuild"

# --- 3. wait for running ----------------------------------------------------
say "3/4 wait for server running"
for i in $(seq 1 60); do
	s="$(api_ro GET "/servers/${HCLOUD_SERVER_ID}")"
	[ "$(json_get "$s" status)" = "running" ] && { ok "server running"; break; }
	log "  ... server status=$(json_get "$s" status) (${i}/60)"; sleep 5
done

# --- 4. inject our key into the installed system via rescue -----------------
say "4/4 inject deploy key via rescue (rebuild cannot inject a new key)"
inject_key_via_rescue

say "provision complete — clean ${XTK_IMAGE} up, deploy key authorized"
