#!/bin/bash
# Migrate a LEGACY single-docker site to the multi-vhost layout, content-preserving:
# relocate its docker-compose.yml/nginx.conf/db.env/.xtk-stack into .vhosts/httpdocs/,
# fix the relative refs that moved, add a per-vhost logs mount, then recreate the
# container under the SAME project name (httpdocs keeps the legacy project `<name>` and
# alias `<name>.site`, so the already-published gateway backend keeps working — no
# re-publish). Idempotent (no-op if already migrated). Rolls back on validation failure.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost_valid_name "$name" || { echo "invalid name" >&2; exit 2; }
dir="$XTK_SITES/$name"
[ -d "$dir/.vhosts" ] && { echo "already migrated: $name"; exit 0; }         # idempotent
[ -f "$dir/docker-compose.yml" ] || { echo "not a legacy site: $name" >&2; exit 3; }
uid="$(id -u "site-$name" 2>/dev/null || stat -c '%u' "$dir")"
gid="$(getent group docker-hosting | cut -d: -f3)"
vdir="$dir/.vhosts/httpdocs"; logs="$dir/logs/httpdocs"; bak="$dir/.migrate-bak"

rm -rf "$bak"; mkdir -p "$bak"
for fn in docker-compose.yml nginx.conf db.env .xtk-stack; do
  [ -f "$dir/$fn" ] && cp -a "$dir/$fn" "$bak/$fn"
done
rollback() {
  rm -rf "$vdir" "$logs"
  for fn in docker-compose.yml nginx.conf db.env .xtk-stack; do
    [ -f "$bak/$fn" ] && cp -a "$bak/$fn" "$dir/$fn"
  done
  rm -rf "$bak"
}

mkdir -p "$vdir" "$logs"
for fn in docker-compose.yml nginx.conf db.env .xtk-stack; do
  [ -f "$dir/$fn" ] && mv "$dir/$fn" "$vdir/$fn"
done
[ -f "$vdir/db.env" ] || : > "$vdir/db.env"   # ensure the env_file target exists

cf="$vdir/docker-compose.yml"
# the docroot mount (./httpdocs) resolves from the site root unchanged; only nginx.conf
# and db.env moved into .vhosts/httpdocs/, so repoint those refs.
sed -i "s#\./nginx\.conf:#./.vhosts/httpdocs/nginx.conf:#g; s#\./db\.env#./.vhosts/httpdocs/db.env#g" "$cf"
# add a per-vhost logs mount next to the nginx.conf mount, if not already present
grep -q "/var/log/nginx" "$cf" || sed -i "/etc\/nginx\/conf\.d\/default\.conf/a\\      - ./logs/httpdocs:/var/log/nginx" "$cf"
# make nginx write to the mounted logs (idempotent)
grep -q "access_log" "$vdir/nginx.conf" 2>/dev/null || \
  sed -i "/listen 8080/a\\    access_log /var/log/nginx/access.log;\\n    error_log  /var/log/nginx/error.log warn;" "$vdir/nginx.conf" 2>/dev/null || true

chown -R root:root "$vdir"; chmod 0755 "$vdir"
chown -R "$uid:$gid" "$logs"
[ -d "$dir/httpdocs" ] && chown -R "$uid:$gid" "$dir/httpdocs" || true

if ! docker compose --project-directory "$dir" -f "$cf" config >/tmp/xtk_mig.out 2>&1; then
  rollback; echo "migrate: invalid compose after relocation (rolled back):" >&2; tail -c 1500 /tmp/xtk_mig.out >&2; exit 4
fi
if ! docker compose --project-directory "$dir" -f "$cf" -p "$name" up -d >/tmp/xtk_mig.out 2>&1; then
  rollback; echo "migrate: apply failed (rolled back):" >&2; tail -c 1500 /tmp/xtk_mig.out >&2; exit 5
fi
rm -rf "$bak"
echo "migrated site=$name -> vhost httpdocs (project $name, alias $name.site)"
