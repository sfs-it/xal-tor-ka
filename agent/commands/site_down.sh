#!/bin/bash
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
dir="$XTK_SITES/$name"; [ -f "$dir/docker-compose.yml" ] || { echo "no such site: $name" >&2; exit 3; }
cd "$dir"; docker compose --project-name "$name" down
