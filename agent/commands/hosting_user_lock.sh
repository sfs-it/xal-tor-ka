#!/bin/bash
set -euo pipefail
: "${XTK_SSH:=/opt/xtk-ssh}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
locked="${XTK_P_LOCKED:?}"; [[ "$locked" =~ ^(true|false)$ ]] || { echo "invalid flag" >&2; exit 2; }
user="site-$name"; f="$XTK_SSH/shadow"; [ -f "$f" ] || { echo "SCP gateway not installed" >&2; exit 4; }
grep -q "^$user:" "$f" || { echo "no scp password set for $user" >&2; exit 3; }
tmp="$(mktemp)"
while IFS= read -r line; do
  case "$line" in
    "$user:"*) IFS=: read -r u h rest <<<"$line"; h="${h#!}"; [ "$locked" = true ] && h="!$h"; printf '%s:%s:%s\n' "$u" "$h" "$rest" ;;
    *) printf '%s\n' "$line" ;;
  esac
done < "$f" > "$tmp"
cat "$tmp" > "$f"; rm -f "$tmp"; chmod 600 "$f"
echo "locked=$locked for $user"
