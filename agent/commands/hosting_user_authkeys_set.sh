#!/bin/bash
# Overwrite the site user's authorized_keys. Content arrives only as env (never a
# shell). User-owned so sshd's privsep child can read it. Also refreshes the SSH
# gateway roster + a '*' shadow entry so key-only users are known to sshd.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"; : "${XTK_SSH:=/opt/xtk-ssh}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
content="${XTK_P_CONTENT-}"
user="site-$name"; dir="$XTK_SITES/$name"; [ -d "$dir" ] || { echo "no such site: $name" >&2; exit 3; }
uid="$(id -u "$user")"; gid="$(id -g "$user")"
install -d -m0700 "$dir/.ssh"; chown "$uid:$gid" "$dir/.ssh"
printf '%s\n' "$content" > "$dir/.ssh/authorized_keys"
chown "$uid:$gid" "$dir/.ssh/authorized_keys"; chmod 0600 "$dir/.ssh/authorized_keys"
if [ -d "$XTK_SSH" ]; then
  hgid="$(getent group docker-hosting | cut -d: -f3)"
  { printf 'root:x:0:0:root:/root:/sbin/nologin\nsshd:x:22:22:sshd:/dev/null:/sbin/nologin\nnobody:x:65534:65534:nobody:/:/sbin/nologin\n'
    getent passwd | awk -F: -v g="$hgid" '$4==g {print $1":x:"$3":"$4":"$5":"$6":/sbin/nologin"}'; } > "$XTK_SSH/passwd"
  grep -q "^$user:" "$XTK_SSH/shadow" 2>/dev/null || { printf '%s:*:20000:0:99999:7:::\n' "$user" >> "$XTK_SSH/shadow"; chmod 600 "$XTK_SSH/shadow"; }
fi
echo "authorized_keys updated for $user"
