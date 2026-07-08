#!/bin/bash
set -euo pipefail
: "${XTK_DB:=/opt/xtk-db}"
engine="${XTK_P_ENGINE:?}"; [[ "$engine" =~ ^(pg|mysql)$ ]] || { echo "invalid engine" >&2; exit 2; }
inst="$XTK_DB/$engine"; proj="xtk-db-$engine"
cid="$(cd "$inst" && docker compose -p "$proj" ps -q db 2>/dev/null || true)"
[ -n "$cid" ] || { echo "[]"; exit 0; }
admin="$(cat "$inst/.admin" 2>/dev/null || true)"
if [ "$engine" = pg ]; then
  dbs="$(docker exec "$cid" psql -U postgres -tAc "SELECT datname FROM pg_database WHERE datistemplate=false AND datname<>'postgres'" 2>/dev/null || true)"
else
  dbs="$(docker exec -e MYSQL_PWD="$admin" "$cid" mariadb -uroot -N -e "SHOW DATABASES" 2>/dev/null | grep -vE '^(information_schema|mysql|performance_schema|sys)$' || true)"
fi
first=1; printf '['
while IFS= read -r d; do [ -z "$d" ] && continue; [ $first -eq 1 ] || printf ','; first=0; printf '"%s"' "$d"; done <<< "$dbs"
printf ']\n'
