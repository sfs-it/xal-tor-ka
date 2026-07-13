#!/bin/bash
# Vetted: create the per-site OS user (in docker-hosting), the root-owned site dir
# (the SFTP chroot), and the FIRST vhost "httpdocs". Does NOT start it (see site_up /
# vhost_up). Params arrive as XTK_P_*.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?name required}"
tmpl="${XTK_P_TEMPLATE:-php-fpm}"
pv="${XTK_P_PHP_VERSION:-8.3}"
# defense in depth (agent already validated via the manifest)
vhost_valid_name "$name"        || { echo "invalid site name"  >&2; exit 2; }
[[ "$tmpl" =~ ^[a-z0-9-]+$ ]]   || { echo "invalid template"   >&2; exit 2; }
[[ "$pv" =~ ^[0-9]+\.[0-9]+$ ]] || { echo "invalid php_version" >&2; exit 2; }
[ -d "$XTK_TEMPLATES/$tmpl" ]   || { echo "unknown template: $tmpl" >&2; exit 2; }
dir="$XTK_SITES/$name"; user="site-$name"; grp="docker-hosting"
[ -e "$dir" ] && { echo "site already exists: $dir" >&2; exit 3; }

getent group "$grp" >/dev/null || groupadd --system "$grp"
id "$user" >/dev/null 2>&1 || useradd --system --no-create-home --home-dir "$dir" \
    --shell /usr/sbin/nologin --gid "$grp" "$user"
uid="$(id -u "$user")"; gid="$(getent group "$grp" | cut -d: -f3)"

# the site dir is the SFTP ChrootDirectory (%h): must be root-owned and not group/world
# writable. Only the per-vhost docroots + logs dirs (made by render_vhost) are user-owned.
mkdir -p "$dir"; chown root:root "$dir"; chmod 0755 "$dir"
docker network inspect xtk-hosting >/dev/null 2>&1 || docker network create xtk-hosting >/dev/null

al="$(render_vhost "$name" "httpdocs" "$tmpl" "$pv" "$uid" "$gid")" || exit $?
echo "site=$name user=$user uid=$uid gid=$gid dir=$dir vhost=httpdocs upstream=http://$al:8080"
