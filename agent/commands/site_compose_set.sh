#!/bin/bash
# Overwrite a site's compose with admin-supplied YAML. The content arrives ONLY as an
# env var (XTK_P_CONTENT), never through a shell — no command injection. It is a
# purpose-specific capability (edit ONE site's compose), gated to the trusted admin;
# the content itself is validated with `docker compose config` and reverted if invalid.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
content="${XTK_P_CONTENT:?}"
dir="$XTK_SITES/$name"; f="$dir/docker-compose.yml"
[ -f "$f" ] || { echo "no such site: $name" >&2; exit 3; }
own="$(stat -c '%u:%g' "$f")"
cp -a "$f" "$f.bak"
printf '%s' "$content" > "$f"
if ! (cd "$dir" && docker compose config >/tmp/xtk_cfg.out 2>&1); then
  cp -a "$f.bak" "$f"; rm -f "$f.bak"
  echo "invalid compose (reverted):" >&2; tail -c 2000 /tmp/xtk_cfg.out >&2; exit 4
fi
chown "$own" "$f" 2>/dev/null || true
rm -f "$f.bak"
(cd "$dir" && docker compose -p "$name" up -d) >/tmp/xtk_up.out 2>&1 || { echo "saved, but apply failed:" >&2; tail -c 1500 /tmp/xtk_up.out >&2; exit 5; }
echo "compose updated and applied for $name"
