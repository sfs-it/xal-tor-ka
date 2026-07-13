#!/bin/bash
# Vetted: add a vhost (its own docker) to an EXISTING site. Does NOT start it (see vhost_up).
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"
tmpl="${XTK_P_TEMPLATE:-php-fpm}"; pv="${XTK_P_PHP_VERSION:-8.3}"
vhost_valid_name "$name"        || { echo "invalid site name"   >&2; exit 2; }
vhost_valid_name "$vhost"       || { echo "invalid vhost name"  >&2; exit 2; }
[[ "$tmpl" =~ ^[a-z0-9-]+$ ]]   || { echo "invalid template"    >&2; exit 2; }
[[ "$pv" =~ ^[0-9]+\.[0-9]+$ ]] || { echo "invalid php_version" >&2; exit 2; }
dir="$XTK_SITES/$name"; user="site-$name"
id "$user" >/dev/null 2>&1 || { echo "no such site: $name" >&2; exit 3; }
[ -d "$dir" ]              || { echo "no such site dir: $name" >&2; exit 3; }
uid="$(id -u "$user")"; gid="$(getent group docker-hosting | cut -d: -f3)"
docker network inspect xtk-hosting >/dev/null 2>&1 || docker network create xtk-hosting >/dev/null
al="$(render_vhost "$name" "$vhost" "$tmpl" "$pv" "$uid" "$gid")" || exit $?
echo "site=$name vhost=$vhost template=$tmpl uid=$uid gid=$gid docroot=$dir/$vhost upstream=http://$al:8080"
