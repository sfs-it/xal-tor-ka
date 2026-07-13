#!/bin/bash
# Write a vhost's db.env (referenced by its php service's env_file). Content arrives ONLY
# as an env var (never a shell). Used to inject DB connection creds. Stays root-owned
# (mounted read-only into the container).
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"; content="${XTK_P_CONTENT:?}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
vdir="$XTK_SITES/$name/.vhosts/$vhost"
[ -d "$vdir" ] || { echo "no such vhost: $name/$vhost" >&2; exit 3; }
printf '%s\n' "$content" > "$vdir/db.env"
chown root:root "$vdir/db.env"; chmod 640 "$vdir/db.env"
echo "db.env written for $name/$vhost"
