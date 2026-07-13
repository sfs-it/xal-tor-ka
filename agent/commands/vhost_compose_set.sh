#!/bin/bash
# Overwrite a vhost's compose with admin-supplied YAML. The content arrives ONLY as an
# env var (XTK_P_CONTENT), never through a shell — no command injection. Validated with
# `docker compose config` (from the site root, so relative paths resolve) and reverted
# if invalid, then applied with up -d under the vhost's compose project.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"; content="${XTK_P_CONTENT:?}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
dir="$XTK_SITES/$name"; f="$dir/.vhosts/$vhost/docker-compose.yml"
[ -f "$f" ] || { echo "no such vhost: $name/$vhost" >&2; exit 3; }
proj="$(vhost_project "$name" "$vhost")"; own="$(stat -c '%u:%g' "$f")"
cp -a "$f" "$f.bak"
printf '%s' "$content" > "$f"
if ! docker compose --project-directory "$dir" -f "$f" config >/tmp/xtk_cfg.out 2>&1; then
  cp -a "$f.bak" "$f"; rm -f "$f.bak"
  echo "invalid compose (reverted):" >&2; tail -c 2000 /tmp/xtk_cfg.out >&2; exit 4
fi
chown "$own" "$f" 2>/dev/null || true
rm -f "$f.bak"
docker compose --project-directory "$dir" -f "$f" -p "$proj" up -d >/tmp/xtk_up.out 2>&1 \
  || { echo "saved, but apply failed:" >&2; tail -c 1500 /tmp/xtk_up.out >&2; exit 5; }
echo "compose updated and applied for $name/$vhost"
