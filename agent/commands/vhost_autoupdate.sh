#!/bin/bash
# Toggle the auto_update flag in a vhost's .xtk-stack record.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"; enabled="${XTK_P_ENABLED:?}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
[[ "$enabled" =~ ^(true|false)$ ]] || { echo "invalid flag" >&2; exit 2; }
f="$XTK_SITES/$name/.vhosts/$vhost/.xtk-stack"
[ -f "$f" ] || { echo "no stack metadata for $name/$vhost" >&2; exit 3; }
own="$(stat -c '%u:%g' "$f")"
grep -v '^auto_update=' "$f" > "$f.tmp" 2>/dev/null || true
echo "auto_update=$enabled" >> "$f.tmp"
mv "$f.tmp" "$f"; chown "$own" "$f"
echo "auto_update=$enabled for $name/$vhost"
