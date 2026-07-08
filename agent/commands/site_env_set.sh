#!/bin/bash
# Write the site's db.env (referenced by the php service's env_file). Content arrives
# ONLY as an env var (never a shell). Used to inject DB connection creds.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
content="${XTK_P_CONTENT:?}"
dir="$XTK_SITES/$name"; [ -d "$dir" ] || { echo "no such site: $name" >&2; exit 3; }
own="$(stat -c '%u:%g' "$dir")"
printf '%s\n' "$content" > "$dir/db.env"
chown "$own" "$dir/db.env"; chmod 640 "$dir/db.env"
echo "db.env written for $name"
