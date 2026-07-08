#!/bin/bash
set -euo pipefail
: "${XTK_DB:=/opt/xtk-db}"
engine="${XTK_P_ENGINE:?}"; [[ "$engine" =~ ^(pg|mysql)$ ]] || { echo "invalid engine" >&2; exit 2; }
inst="$XTK_DB/$engine"; proj="xtk-db-$engine"
installed=false; running=false; ver=""
[ "$engine" = pg ] && port=5432 || port=3306
if [ -f "$inst/docker-compose.yml" ]; then
  installed=true
  cid="$(cd "$inst" && docker compose -p "$proj" ps -q db 2>/dev/null || true)"
  if [ -n "$cid" ] && [ "$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null)" = true ]; then
    running=true
    if [ "$engine" = pg ]; then ver="$(docker exec "$cid" postgres --version 2>/dev/null | awk '{print $NF}')"
    else ver="$(docker exec "$cid" mariadb --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"; fi
  fi
fi
printf '{"engine":"%s","installed":%s,"running":%s,"host":"xtk-db-%s.db","port":%s,"localhost":"127.0.0.1:%s","version":"%s"}\n' \
  "$engine" "$installed" "$running" "$engine" "$port" "$port" "$ver"
