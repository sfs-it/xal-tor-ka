# shellcheck shell=bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# Shared library for the Hetzner provisioning/deploy scripts. Sourced, never run.
#
# Design invariants (paid for in blood, see the house guardrails):
#   * DRY-RUN BY DEFAULT. Nothing destructive or remote runs unless --apply
#     (or DRY_RUN=0) is passed. A read-only API GET is always allowed: that is
#     how "verify the information is correct" works without touching anything.
#   * IT GROANS ON FAILURE. set -euo pipefail + an ERR trap that prints the
#     failing line. A script must never fail silently (the bolero lesson).
#   * NO SECRET IS HARDCODED. This repository is public. Everything comes from
#     the env file (default: $XTK_HETZNER_ENV), which lives OUTSIDE the repo.
#   * EVERY FACT IS ASSERTED, NOT TRUSTED. State is read back from the API /
#     the remote host, never assumed.

set -euo pipefail

# ---- paths & env -----------------------------------------------------------
HZ_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
: "${XTK_HETZNER_ENV:=${HZ_DIR}/hetzner.env}"   # override to point at the house secret
# The deploy SSH key lives ALONGSIDE the env file (i.e. in the operator's secrets
# store, outside this public repo) — never inside the repo tree.
: "${XTK_SSH_KEY:=$(dirname "$XTK_HETZNER_ENV")/hetzner_ed25519}"
: "${DRY_RUN:=1}"                                # 1 = dry-run (default), 0 = apply
DATETIME="$(date +%Y%m%d-%H%M)"
: "${XTK_HETZNER_LOGDIR:=/tmp/xtk-hetzner-logs}"
LOGFILE="${XTK_HETZNER_LOGDIR}/hetzner-$(basename "${0%.sh}")-${DATETIME}.log"

HCLOUD_API="https://api.hetzner.cloud/v1"

# ---- colours / logging -----------------------------------------------------
_c()  { printf '\033[%sm' "$1"; }
_rst(){ printf '\033[0m'; }
log()  { mkdir -p "$XTK_HETZNER_LOGDIR"; printf '%s\n' "$(date +%H:%M:%S) $*" | tee -a "$LOGFILE" >&2; }
say()  { log "$(_c '1;36')==>$(_rst) $*"; }
ok()   { log "$(_c '1;32')  ok$(_rst) $*"; }
warn() { log "$(_c '1;33')  !!$(_rst) $*"; }
die()  { log "$(_c '1;31')FAIL$(_rst) $*"; exit 1; }

_on_err() { local rc=$?; log "$(_c '1;31')FAIL$(_rst) line $1 exited $rc (see $LOGFILE)"; exit "$rc"; }
trap '_on_err $LINENO' ERR

dry() { [ "${DRY_RUN}" = "1" ]; }
mode_banner() { if dry; then warn "DRY-RUN (nothing is changed — pass --apply or DRY_RUN=0 to execute)";
                else warn "APPLY MODE (changes WILL be made)"; fi; }

# ---- env loading & validation ----------------------------------------------
load_env() {
	[ -f "$XTK_HETZNER_ENV" ] || die "env file not found: $XTK_HETZNER_ENV (set XTK_HETZNER_ENV)"
	local perm; perm=$(stat -c '%a' "$XTK_HETZNER_ENV")
	[ "$perm" = "600" ] || warn "env file $XTK_HETZNER_ENV is $perm, expected 600"
	# Tolerate CRLF (Windows/WSL edits): source from a CR-stripped copy, never
	# rewriting the operator's file. A stray \r would otherwise corrupt values.
	local tmp; tmp="$(mktemp)"
	if grep -q $'\r' "$XTK_HETZNER_ENV"; then warn "env has CRLF line endings — stripping CR for this run only"; fi
	tr -d '\r' < "$XTK_HETZNER_ENV" > "$tmp"
	# shellcheck disable=SC1090
	set -a; . "$tmp"; set +a
	rm -f "$tmp"
	ok "env loaded from $XTK_HETZNER_ENV"
}

require_vars() {
	local missing=() v
	for v in "$@"; do [ -n "${!v:-}" ] || missing+=("$v"); done
	[ ${#missing[@]} -eq 0 ] || die "missing required env vars: ${missing[*]}"
}

# ---- Hetzner Cloud API -----------------------------------------------------
# api_ro METHOD PATH        -> always executed (use ONLY for GET / read-only)
# api_w  METHOD PATH [BODY] -> executed only in apply mode; dry-run just prints
# Both echo the response body on stdout and fail (die) on HTTP >= 400.
_api() {
	local method="$1" path="$2" body="${3:-}" tmp code
	tmp="$(mktemp)"
	if [ -n "$body" ]; then
		code=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" \
			-H "Authorization: Bearer ${HCLOUD_TOKEN}" \
			-H "Content-Type: application/json" \
			-d "$body" "${HCLOUD_API}${path}")
	else
		code=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" \
			-H "Authorization: Bearer ${HCLOUD_TOKEN}" "${HCLOUD_API}${path}")
	fi
	local out; out="$(cat "$tmp")"; rm -f "$tmp"
	if [ "$code" -ge 400 ]; then
		printf '%s\n' "$out" >&2
		die "API $method $path -> HTTP $code"
	fi
	printf '%s' "$out"
}
api_ro() { _api "$1" "$2"; }
api_w()  {
	if dry; then
		local b="${3:-}"; log "$(_c '1;35')DRY$(_rst) would $1 ${HCLOUD_API}$2${b:+  body=$b}"
		printf '%s' '{"__dry_run__":true}'
	else
		_api "$1" "$2" "${3:-}"
	fi
}

# tiny JSON field extractor (no jq dependency; good enough for scalar fields).
# Never propagates a non-zero rc: a missing field yields an empty string, so a
# no-match grep inside $(json_get ...) cannot trip set -e (the whole reason this
# stayed a latent killer until the first field was genuinely absent).
json_get() { # json_get '<json>' '<key>'  -> first scalar value for key (or '')
	printf '%s' "$1" | grep -oE "\"$2\"[[:space:]]*:[[:space:]]*(\"[^\"]*\"|[0-9]+|true|false|null)" \
		| head -1 | sed -E "s/.*:[[:space:]]*//; s/^\"//; s/\"$//" || true
}

# ---- remote (SSH) ----------------------------------------------------------
# ssh_run "command"   -> runs on the target as root; dry-run prints instead.
ssh_opts() {
	# A rebuild gives the box a NEW host key while the old one may linger in
	# known_hosts (accept-new would REJECT a *changed* key). For provisioning a
	# machine we own, skip host-key state entirely — same choice as scripts/deploy.sh.
	printf '%s\n' -i "$XTK_SSH_KEY" -o StrictHostKeyChecking=no \
		-o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
		-o ConnectTimeout=20 -o ServerAliveInterval=15 -o ServerAliveCountMax=40
}
ssh_run() {
	require_vars HCLOUD_SERVER_IP
	local cmd="$*"
	if dry; then
		log "$(_c '1;35')DRY$(_rst) would ssh root@${HCLOUD_SERVER_IP}: ${cmd}"
	else
		mapfile -t _o < <(ssh_opts)
		ssh "${_o[@]}" "root@${HCLOUD_SERVER_IP}" "$cmd"
	fi
}

# ---- action polling & SSH readiness ----------------------------------------
poll_action() { # poll_action <action_id> [label] — wait success/error/timeout
	local id="$1" label="${2:-action}" a st
	for i in $(seq 1 60); do
		a="$(api_ro GET "/actions/${id}")"; st="$(json_get "$a" status)"
		case "$st" in
			success) ok "${label} completed"; return 0 ;;
			error)   die "${label} failed: $a" ;;
			*)       log "  ... ${label} status=${st} (${i}/60)"; sleep 5 ;;
		esac
	done
	die "${label} did not complete within timeout"
}

wait_ssh_key() { # wait until our KEY logs in (installed system or rescue)
	local tries="${1:-40}" i
	mapfile -t _o < <(ssh_opts)
	for i in $(seq 1 "$tries"); do
		ssh "${_o[@]}" -o BatchMode=yes "root@${HCLOUD_SERVER_IP}" true 2>/dev/null && { ok "SSH up (key) on ${HCLOUD_SERVER_IP}"; return 0; }
		log "  ... waiting for key-SSH (${i}/${tries})"; sleep 6
	done
	die "key-SSH never answered on ${HCLOUD_SERVER_IP}"
}

# ---- key injection via RESCUE (rebuild does NOT inject a new key) -----------
# Hetzner `rebuild` re-applies only the keys bound at CREATE; a newly registered
# key never lands on the disk, and Ubuntu images have PasswordAuthentication=no
# so reset_password is console-only. The robust, IP/id/type-preserving fix:
# enable_rescue WITH our ssh key (it works in rescue) -> reboot -> mount the
# installed root -> append our pubkey there -> disable_rescue -> reboot.
inject_key_via_rescue() {
	require_vars HCLOUD_TOKEN HCLOUD_SERVER_ID HCLOUD_SERVER_IP
	local pub keyid resp aid
	pub="$(cat "${XTK_SSH_KEY}.pub")"
	local KEY_NAME="${HCLOUD_SSH_KEY_NAME:-custode-deploy}"

	say "rescue 1/6 resolve ssh key id for '${KEY_NAME}'"
	keyid="$(json_get "$(api_ro GET "/ssh_keys?name=${KEY_NAME}")" id)"
	[ -n "$keyid" ] || die "ssh key '${KEY_NAME}' not registered (run the key step first)"
	ok "ssh key id=${keyid}"

	say "rescue 2/6 enable_rescue (linux64) with our key"
	resp="$(api_w POST "/servers/${HCLOUD_SERVER_ID}/actions/enable_rescue" \
		"$(printf '{"type":"linux64","ssh_keys":[%s]}' "$keyid")")"
	dry && { say "dry-run: skipping rescue reboot/mount"; return 0; }
	aid="$(json_get "$resp" id)"; poll_action "$aid" "enable_rescue"

	say "rescue 3/6 reset (reboot into rescue)"
	aid="$(json_get "$(api_w POST "/servers/${HCLOUD_SERVER_ID}/actions/reset" '')" id)"
	poll_action "$aid" "reset->rescue"
	sleep 10; wait_ssh_key 40

	say "rescue 4/6 mount installed root + inject pubkey"
	local remote
	remote="$(cat <<REMOTE
set -e
root=""
for d in /dev/sda1 /dev/sda2 /dev/sda3 /dev/vda1 /dev/vda2; do
  [ -b "\$d" ] || continue
  mount "\$d" /mnt 2>/dev/null || continue
  if [ -f /mnt/etc/os-release ] && [ -d /mnt/root ]; then root="\$d"; break; fi
  umount /mnt 2>/dev/null || true
done
[ -n "\$root" ] || { echo NO_ROOT_FS; exit 1; }
f=/mnt/root/.ssh/authorized_keys
mkdir -p /mnt/root/.ssh; touch "\$f"
# newline-safety: a pre-existing authorized_keys (e.g. seeded by an addon) may
# lack a trailing newline; a leading \n on append guarantees our key lands on its
# own line (a blank line is ignored by sshd). Without it our key fuses onto the
# last existing key and sshd rejects both.
grep -qxF "${pub}" "\$f" || printf '\n%s\n' "${pub}" >> "\$f"
chmod 700 /mnt/root/.ssh; chmod 600 "\$f"
sync; umount /mnt
echo "MOUNT_INJECT_OK root=\$root"
REMOTE
)"
	mapfile -t _o < <(ssh_opts)
	ssh "${_o[@]}" "root@${HCLOUD_SERVER_IP}" "$remote" | tee -a "$LOGFILE" | grep -q MOUNT_INJECT_OK \
		|| die "rescue mount/inject failed"
	ok "pubkey injected into installed system"

	say "rescue 5/6 disable_rescue"
	aid="$(json_get "$(api_w POST "/servers/${HCLOUD_SERVER_ID}/actions/disable_rescue" '')" id)"
	poll_action "$aid" "disable_rescue"

	say "rescue 6/6 reset (reboot into installed system) + verify key login"
	aid="$(json_get "$(api_w POST "/servers/${HCLOUD_SERVER_ID}/actions/reset" '')" id)"
	poll_action "$aid" "reset->installed"
	sleep 10; wait_ssh_key 40
	ok "key access to the installed system confirmed"
}
