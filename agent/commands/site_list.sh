#!/bin/bash
# Read-only inventory of provisioned sites, emitted as a JSON array. Site names are
# created by site_create under a strict pattern, so embedding them in JSON is safe.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
first=1; printf '['
if [ -d "$XTK_SITES" ]; then
  for d in "$XTK_SITES"/*/; do
    [ -f "$d/docker-compose.yml" ] || continue
    name="$(basename "$d")"
    running="$(cd "$d" && docker compose -p "$name" ps -q 2>/dev/null | wc -l | tr -d ' ')"
    uid="$(stat -c '%u' "$d")"
    tmpl=""; pv=""
    if [ -f "$d/.xtk-stack" ]; then
      tmpl="$(sed -n 's/^template=//p' "$d/.xtk-stack")"
      pv="$(sed -n 's/^php_version=//p' "$d/.xtk-stack")"
    fi
    [ $first -eq 1 ] || printf ','; first=0
    printf '{"name":"%s","uid":%s,"running":%s,"template":"%s","php_version":"%s"}' "$name" "$uid" "$running" "$tmpl" "$pv"
  done
fi
printf ']\n'
