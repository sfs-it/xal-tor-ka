#!/bin/bash
set -euo pipefail
: "${XTK_SSH:=/opt/xtk-ssh}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
password="${XTK_P_PASSWORD:?}"; [ ${#password} -ge 8 ] || { echo "password too short (min 8)" >&2; exit 2; }
user="site-$name"; id "$user" >/dev/null 2>&1 || { echo "no such user: $user" >&2; exit 3; }
[ -f "$XTK_SSH/shadow" ] || { echo "SCP gateway not installed — bring it up first" >&2; exit 4; }
hash="$(printf '%s' "$password" | openssl passwd -6 -stdin)"
tmp="$(mktemp)"; grep -v "^$user:" "$XTK_SSH/shadow" > "$tmp" 2>/dev/null || true
printf '%s:%s:20000:0:99999:7:::\n' "$user" "$hash" >> "$tmp"
cat "$tmp" > "$XTK_SSH/shadow"; rm -f "$tmp"; chmod 600 "$XTK_SSH/shadow"
# refresh the roster so a just-created site is known to sshd (files are live-mounted)
hgid="$(getent group docker-hosting | cut -d: -f3)"
{ printf 'root:x:0:0:root:/root:/sbin/nologin\nsshd:x:22:22:sshd:/dev/null:/sbin/nologin\nnobody:x:65534:65534:nobody:/:/sbin/nologin\n'
  getent passwd | awk -F: -v g="$hgid" '$4==g {print $1":x:"$3":"$4":"$5":"$6":/sbin/nologin"}'; } > "$XTK_SSH/passwd"
echo "scp password set for $user"
