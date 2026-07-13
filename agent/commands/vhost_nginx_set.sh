#!/bin/bash
# Replace a vhost's nginx.conf. Content arrives only as env (never a shell). Validated
# with 'nginx -t' in the vhost's running web container (reverted if invalid), then reloaded.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"; content="${XTK_P_CONTENT:?}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
dir="$XTK_SITES/$name"; f="$dir/.vhosts/$vhost/nginx.conf"
[ -f "$f" ] || { echo "no nginx.conf for $name/$vhost" >&2; exit 3; }
proj="$(vhost_project "$name" "$vhost")"; cf="$dir/.vhosts/$vhost/docker-compose.yml"
own="$(stat -c '%u:%g' "$f")"
cp -a "$f" "$f.bak"
printf '%s' "$content" > "$f"; chown "$own" "$f"
web="$(docker compose --project-directory "$dir" -f "$cf" -p "$proj" ps -q web 2>/dev/null || true)"
if [ -n "$web" ] && [ "$(docker inspect -f '{{.State.Running}}' "$web" 2>/dev/null)" = true ]; then
  if docker exec "$web" nginx -t >/tmp/xtk_ngx.out 2>&1; then
    docker exec "$web" nginx -s reload >/dev/null 2>&1 || true
    rm -f "$f.bak"; echo "nginx.conf updated and reloaded for $name/$vhost"
  else
    cp -a "$f.bak" "$f"; chown "$own" "$f"; rm -f "$f.bak"
    echo "invalid nginx config (reverted):" >&2; tail -c 1500 /tmp/xtk_ngx.out >&2; exit 4
  fi
else
  rm -f "$f.bak"; echo "nginx.conf saved for $name/$vhost (not running — applied on next start)"
fi
