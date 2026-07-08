#!/bin/bash
# Vetted: create the per-site OS user (in docker-hosting), the site dir, and the
# rendered compose. Does NOT start it (see site_up). Params arrive as XTK_P_*.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
: "${XTK_TEMPLATES:=/usr/local/lib/xtk-agent/templates}"
name="${XTK_P_NAME:?name required}"
tmpl="${XTK_P_TEMPLATE:-php-fpm}"
pv="${XTK_P_PHP_VERSION:-8.3}"
# defense in depth (agent already validated via the manifest)
[[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid site name" >&2; exit 2; }
[[ "$tmpl" =~ ^[a-z0-9-]+$ ]]            || { echo "invalid template"  >&2; exit 2; }
[[ "$pv" =~ ^[0-9]+\.[0-9]+$ ]]          || { echo "invalid php_version" >&2; exit 2; }
src="$XTK_TEMPLATES/$tmpl"; dir="$XTK_SITES/$name"; user="site-$name"; grp="docker-hosting"
[ -d "$src" ] || { echo "unknown template: $tmpl" >&2; exit 2; }
[ -e "$dir" ] && { echo "site already exists: $dir" >&2; exit 3; }

getent group "$grp" >/dev/null || groupadd --system "$grp"
id "$user" >/dev/null 2>&1 || useradd --system --no-create-home --home-dir "$dir" \
    --shell /usr/sbin/nologin --gid "$grp" "$user"
uid="$(id -u "$user")"; gid="$(getent group "$grp" | cut -d: -f3)"

mkdir -p "$dir"; cp -a "$src/." "$dir/"
for f in "$dir/docker-compose.yml" "$dir/nginx.conf" "$dir/www/index.php" "$dir/www/index.html"; do
  [ -f "$f" ] && sed -i "s|__NAME__|$name|g; s|__UID__|$uid|g; s|__GID__|$gid|g; s|__PHP_VERSION__|$pv|g" "$f"
done
chown -R "$uid:$gid" "$dir"
docker network inspect xtk-hosting >/dev/null 2>&1 || docker network create xtk-hosting >/dev/null

echo "site=$name user=$user uid=$uid gid=$gid dir=$dir upstream=http://$name.site:8080"
