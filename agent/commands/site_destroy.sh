#!/bin/bash
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
dir="$XTK_SITES/$name"; user="site-$name"
[ -d "$dir" ] || { echo "no such site: $name" >&2; exit 3; }
[ -f "$dir/docker-compose.yml" ] && (cd "$dir" && docker compose -p "$name" down -v) || true
rm -rf "$dir"
id "$user" >/dev/null 2>&1 && userdel "$user" || true
echo "destroyed site=$name"
