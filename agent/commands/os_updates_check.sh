#!/bin/bash
# Read-only: refresh the apt package lists and report available OS updates as JSON.
# Output: {"total":N,"security":M,"reboot_required":bool,"packages":[{name,current,candidate,security}]}
# No params. Runs as root (the agent's trusted uid) — apt-get update needs it.
# Debian/apt only for now; on a non-apt host it reports total 0 (best-effort).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

if ! command -v apt-get >/dev/null 2>&1; then
  printf '{"total":0,"security":0,"reboot_required":false,"packages":[],"note":"apt not available"}\n'
  exit 0
fi

# Best-effort refresh — a single flaky mirror must not fail the whole check.
apt-get update -qq >/dev/null 2>&1 || true

reboot=false
[ -f /var/run/reboot-required ] && reboot=true

# held packages ("no update") — space-padded set for whole-word matching.
held_set=" $(apt-mark showhold 2>/dev/null | tr '\n' ' ') "

total=0; sec=0; held=0; pkgs=""; first=1
# `apt list --upgradable` lines: "name/suite[,suite] candidate arch [upgradable from: current]"
while IFS= read -r line; do
  [ -n "$line" ] || continue
  name="${line%%/*}"
  suite="${line#*/}"; suite="${suite%% *}"
  candidate="$(printf '%s' "$line" | awk '{print $2}')"
  current="$(printf '%s' "$line" | sed -n 's/.*upgradable from: \([^]]*\)].*/\1/p')"
  is_sec=false
  case "$suite" in *-security*|*Security*) is_sec=true; sec=$((sec+1)) ;; esac
  is_held=false
  case "$held_set" in *" $name "*) is_held=true; held=$((held+1)) ;; esac
  total=$((total+1))
  [ "$first" -eq 1 ] || pkgs+=','
  first=0
  pkgs+="$(printf '{"name":"%s","current":"%s","candidate":"%s","security":%s,"held":%s}' \
    "$name" "$current" "$candidate" "$is_sec" "$is_held")"
done < <(apt list --upgradable 2>/dev/null | grep -F '[upgradable from:')

printf '{"total":%s,"security":%s,"held":%s,"reboot_required":%s,"packages":[%s]}\n' \
  "$total" "$sec" "$held" "$reboot" "$pkgs"
