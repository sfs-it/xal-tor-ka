#!/bin/bash
# Replace a site's nginx.conf. Content arrives only as env (never a shell). Validated
# with 'nginx -t' in the running web container (reverted if invalid), then reloaded.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
content="${XTK_P_CONTENT:?}"
dir="$XTK_SITES/$name"; f="$dir/nginx.conf"; [ -f "$f" ] || { echo "no nginx.conf for $name" >&2; exit 3; }
own="$(stat -c '%u:%g' "$f")"
cp -a "$f" "$f.bak"
printf '%s' "$content" > "$f"; chown "$own" "$f"
web="$(cd "$dir" && docker compose -p "$name" ps -q web 2>/dev/null || true)"
if [ -n "$web" ] && [ "$(docker inspect -f '{{.State.Running}}' "$web" 2>/dev/null)" = true ]; then
  if docker exec "$web" nginx -t >/tmp/xtk_ngx.out 2>&1; then
    docker exec "$web" nginx -s reload >/dev/null 2>&1 || true
    rm -f "$f.bak"; echo "nginx.conf updated and reloaded for $name"
  else
    cp -a "$f.bak" "$f"; chown "$own" "$f"; rm -f "$f.bak"
    echo "invalid nginx config (reverted):" >&2; tail -c 1500 /tmp/xtk_ngx.out >&2; exit 4
  fi
else
  rm -f "$f.bak"; echo "nginx.conf saved for $name (site not running — applied on next start)"
fi
