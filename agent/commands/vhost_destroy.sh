#!/bin/bash
# Vetted: remove ONE vhost (its docker + docroot + logs + config). Refuses "httpdocs"
# (the site anchor — use site_destroy for the whole site). Keeps the OS user.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
[ "$vhost" = httpdocs ] && { echo "refusing to destroy the httpdocs vhost; use site_destroy" >&2; exit 4; }
dir="$XTK_SITES/$name"; f="$dir/.vhosts/$vhost/docker-compose.yml"
[ -f "$f" ] || { echo "no such vhost: $name/$vhost" >&2; exit 3; }
docker compose --project-directory "$dir" -f "$f" -p "$(vhost_project "$name" "$vhost")" down -v || true
rm -rf "${dir:?}/.vhosts/${vhost:?}" "${dir:?}/${vhost:?}" "${dir:?}/logs/${vhost:?}"
echo "destroyed vhost=$name/$vhost"
