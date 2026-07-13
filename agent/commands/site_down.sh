#!/bin/bash
# Vetted: stop a site — every vhost (new layout) or the legacy single compose.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost_valid_name "$name" || { echo "invalid name" >&2; exit 2; }
dir="$XTK_SITES/$name"
_down() { docker compose --project-directory "$XTK_SITES/$1" \
            -f "$XTK_SITES/$1/.vhosts/$2/docker-compose.yml" -p "$(vhost_project "$1" "$2")" down; }
if [ -d "$dir/.vhosts" ]; then
  for_each_vhost "$name" _down
elif [ -f "$dir/docker-compose.yml" ]; then
  cd "$dir"; docker compose --project-name "$name" down   # legacy single-docker site
else
  echo "no such site: $name" >&2; exit 3
fi
echo "stopped site=$name"
