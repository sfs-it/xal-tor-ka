#!/bin/bash
# Read-only: report the 'xaltorka' fail2ban jail status as JSON (currently-banned IPs
# and totals). Reports enabled:false if fail2ban / the jail is not present. No params.
set -euo pipefail

if ! command -v fail2ban-client >/dev/null 2>&1; then
  printf '{"enabled":false,"note":"fail2ban not installed"}\n'; exit 0
fi
if ! fail2ban-client status xaltorka >/dev/null 2>&1; then
  printf '{"enabled":false,"note":"jail xaltorka not configured"}\n'; exit 0
fi

st="$(fail2ban-client status xaltorka 2>/dev/null)"
num() { printf '%s' "$st" | sed -n "s/.*$1:[[:space:]]*\([0-9]\+\).*/\1/p" | head -1; }
banned_list="$(printf '%s' "$st" | sed -n 's/.*Banned IP list:[[:space:]]*//p')"
cur="$(num 'Currently banned')"; tot="$(num 'Total banned')"; failc="$(num 'Currently failed')"

items=""; first=1
for ip in $banned_list; do
  [ "$first" -eq 1 ] || items+=','
  first=0; items+="\"$ip\""
done
printf '{"enabled":true,"jail":"xaltorka","currently_banned":%s,"total_banned":%s,"currently_failed":%s,"banned":[%s]}\n' \
  "${cur:-0}" "${tot:-0}" "${failc:-0}" "$items"
