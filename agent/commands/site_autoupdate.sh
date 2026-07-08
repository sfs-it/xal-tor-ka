#!/bin/bash
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
enabled="${XTK_P_ENABLED:?}"; [[ "$enabled" =~ ^(true|false)$ ]] || { echo "invalid flag" >&2; exit 2; }
f="$XTK_SITES/$name/.xtk-stack"; [ -f "$f" ] || { echo "no stack metadata for $name" >&2; exit 3; }
own="$(stat -c '%u:%g' "$f")"
grep -v '^auto_update=' "$f" > "$f.tmp" 2>/dev/null || true
echo "auto_update=$enabled" >> "$f.tmp"
mv "$f.tmp" "$f"; chown "$own" "$f"
echo "auto_update=$enabled for $name"
