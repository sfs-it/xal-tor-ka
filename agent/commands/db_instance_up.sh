#!/bin/bash
set -euo pipefail
: "${XTK_DB:=/opt/xtk-db}"; : "${XTK_TEMPLATES:=/usr/local/lib/xtk-agent/templates}"
engine="${XTK_P_ENGINE:?}"; [[ "$engine" =~ ^(pg|mysql)$ ]] || { echo "invalid engine" >&2; exit 2; }
inst="$XTK_DB/$engine"; proj="xtk-db-$engine"
docker network inspect xtk-hosting >/dev/null 2>&1 || docker network create xtk-hosting >/dev/null
mkdir -p "$inst"
[ -f "$inst/.admin" ] || { head -c32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c24 > "$inst/.admin"; chmod 600 "$inst/.admin"; }
if [ ! -f "$inst/docker-compose.yml" ]; then
  tmpl="$XTK_TEMPLATES/db/$engine/docker-compose.yml"
  [ -f "$tmpl" ] || { echo "no db template for $engine" >&2; exit 5; }
  sed "s|__ADMIN_PW__|$(cat "$inst/.admin")|g" "$tmpl" > "$inst/docker-compose.yml"
fi
(cd "$inst" && docker compose -p "$proj" up -d) >/dev/null 2>&1
echo "instance $engine is up"
