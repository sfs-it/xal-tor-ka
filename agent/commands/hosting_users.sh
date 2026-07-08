#!/bin/bash
# List the OS users that own sites: those whose PRIMARY group is docker-hosting
# (set by site_create's useradd --gid). Primary-group membership lives in passwd,
# not in the group's member list — so we match by gid. Read-only, JSON output.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
grp=docker-hosting
gid="$(getent group "$grp" | cut -d: -f3)"
[ -n "$gid" ] || { echo "[]"; exit 0; }
first=1; printf '['
while IFS=: read -r user _ uid ugid _ home _; do
  [ "$ugid" = "$gid" ] || continue
  site="${user#site-}"
  orphan=false; [ -d "$XTK_SITES/$site" ] || orphan=true
  [ $first -eq 1 ] || printf ','; first=0
  printf '{"user":"%s","uid":%s,"site":"%s","home":"%s","orphan":%s}' "$user" "$uid" "$site" "$home" "$orphan"
done < <(getent passwd)
printf ']\n'
