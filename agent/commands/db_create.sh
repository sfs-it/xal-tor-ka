#!/bin/bash
# Create a database + dedicated user on the SHARED engine instance (pg|mysql),
# bringing the instance up on first use. Values are strictly validated (alnum/_),
# so the SQL string interpolation below is safe. Prints the connection (incl. a
# generated password) on stdout for the caller to inject into the site.
set -euo pipefail
: "${XTK_DB:=/opt/xtk-db}"; : "${XTK_TEMPLATES:=/usr/local/lib/xtk-agent/templates}"
engine="${XTK_P_ENGINE:?}"; name="${XTK_P_NAME:?}"
[[ "$engine" =~ ^(pg|mysql)$ ]]         || { echo "invalid engine" >&2; exit 2; }
[[ "$name" =~ ^[a-z][a-z0-9_]{1,30}$ ]] || { echo "invalid db name" >&2; exit 2; }
inst="$XTK_DB/$engine"; proj="xtk-db-$engine"
docker network inspect xtk-hosting >/dev/null 2>&1 || docker network create xtk-hosting >/dev/null
mkdir -p "$inst"
[ -f "$inst/.admin" ] || { head -c32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c24 > "$inst/.admin"; chmod 600 "$inst/.admin"; }
admin="$(cat "$inst/.admin")"
if [ ! -f "$inst/docker-compose.yml" ]; then
  tmpl="$XTK_TEMPLATES/db/$engine/docker-compose.yml"   # shared source of truth (localhost port)
  [ -f "$tmpl" ] || { echo "no db template for $engine" >&2; exit 5; }
  sed "s|__ADMIN_PW__|$admin|g" "$tmpl" > "$inst/docker-compose.yml"
fi
(cd "$inst" && docker compose -p "$proj" up -d) >/dev/null
cid="$(cd "$inst" && docker compose -p "$proj" ps -q db)"
pw="$(head -c32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c24)"
if [ "$engine" = pg ]; then
  for i in $(seq 1 30); do docker exec "$cid" pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done
  docker exec "$cid" psql -U postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='$name'" | grep -q 1 && { echo "db already exists" >&2; exit 3; }
  docker exec "$cid" psql -v ON_ERROR_STOP=1 -U postgres -c "CREATE ROLE \"$name\" LOGIN PASSWORD '$pw';" >/dev/null
  docker exec "$cid" createdb -U postgres -O "$name" "$name" >/dev/null
  host=xtk-db-pg.db; port=5432
else
  for i in $(seq 1 30); do docker exec -e MYSQL_PWD="$admin" "$cid" mariadb -uroot -e 'SELECT 1' >/dev/null 2>&1 && break; sleep 1; done
  docker exec -e MYSQL_PWD="$admin" "$cid" mariadb -uroot -N -e "SELECT 1 FROM mysql.user WHERE user='$name'" 2>/dev/null | grep -q 1 && { echo "db already exists" >&2; exit 3; }
  docker exec -e MYSQL_PWD="$admin" "$cid" mariadb -uroot -e "CREATE DATABASE \`$name\`; CREATE USER '$name'@'%' IDENTIFIED BY '$pw'; GRANT ALL ON \`$name\`.* TO '$name'@'%'; FLUSH PRIVILEGES;" >/dev/null
  host=xtk-db-mysql.db; port=3306
fi
echo "engine=$engine host=$host port=$port db=$name user=$name password=$pw"
