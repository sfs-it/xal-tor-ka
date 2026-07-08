#!/bin/bash
# Ephemeral Adminer (single-file DB admin) for the shared instance. Reachable only
# by the extension over xtk-hosting (no host port); auth-gating is the admin session.
set -euo pipefail
: "${XTK_DB:=/opt/xtk-db}"
engine="${XTK_P_ENGINE:?}"; [[ "$engine" =~ ^(pg|mysql)$ ]]   || { echo "invalid engine" >&2; exit 2; }
token="${XTK_P_TOKEN:?}";  [[ "$token" =~ ^[a-f0-9]{8,40}$ ]] || { echo "invalid token" >&2; exit 2; }
[ -f "$XTK_DB/$engine/.admin" ] || { echo "$engine instance not installed" >&2; exit 3; }
docker network inspect xtk-hosting >/dev/null 2>&1 || { echo "no xtk-hosting network" >&2; exit 3; }
name="xtk-adminer-$token"
if [ "$engine" = pg ]; then dbhost="xtk-db-pg.db"; dbuser="postgres"; else dbhost="xtk-db-mysql.db"; dbuser="root"; fi
docker inspect "$name" >/dev/null 2>&1 || docker run -d --name "$name" \
  --network xtk-hosting --network-alias "$name.adm" --label xtk-adminer=1 \
  --memory 128m --restart no -e ADMINER_DEFAULT_SERVER="$dbhost" \
  adminer:latest >/dev/null
echo "alias=$name.adm port=8080 server=$dbhost user=$dbuser password=$(cat "$XTK_DB/$engine/.admin")"
