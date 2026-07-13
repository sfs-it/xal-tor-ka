#!/bin/bash
# Vetted: tear a site down completely — every vhost's docker (new layout) or the legacy
# compose — then remove the dir and the OS user.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost_valid_name "$name" || { echo "invalid name" >&2; exit 2; }
dir="$XTK_SITES/$name"; user="site-$name"
[ -d "$dir" ] || { echo "no such site: $name" >&2; exit 3; }
_rm() { docker compose --project-directory "$XTK_SITES/$1" \
          -f "$XTK_SITES/$1/.vhosts/$2/docker-compose.yml" -p "$(vhost_project "$1" "$2")" down -v || true; }
if [ -d "$dir/.vhosts" ]; then
  for_each_vhost "$name" _rm
elif [ -f "$dir/docker-compose.yml" ]; then
  (cd "$dir" && docker compose -p "$name" down -v) || true   # legacy single-docker site
fi
rm -rf "${dir:?}"
id "$user" >/dev/null 2>&1 && userdel "$user" || true
echo "destroyed site=$name"
