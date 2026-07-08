#!/bin/bash
set -euo pipefail
: "${XTK_SSH:=/opt/xtk-ssh}"; : "${XTK_TEMPLATES:=/usr/local/lib/xtk-agent/templates}"
mkdir -p "$XTK_SSH/keys"
for f in Dockerfile sshd_config docker-compose.yml; do
  [ -f "$XTK_SSH/$f" ] || cp "$XTK_TEMPLATES/ssh/$f" "$XTK_SSH/$f"
done
hgid="$(getent group docker-hosting | cut -d: -f3)"; [ -n "$hgid" ] || { echo "no docker-hosting group" >&2; exit 3; }
# synthesized passwd/group: minimal system entries + the site users (nologin shell)
{ printf 'root:x:0:0:root:/root:/sbin/nologin\n'
  printf 'sshd:x:22:22:sshd:/dev/null:/sbin/nologin\n'
  printf 'nobody:x:65534:65534:nobody:/:/sbin/nologin\n'
  getent passwd | awk -F: -v g="$hgid" '$4==g {print $1":x:"$3":"$4":"$5":"$6":/sbin/nologin"}'
} > "$XTK_SSH/passwd"
printf 'root:x:0:\nsshd:x:22:\nnobody:x:65534:\nnogroup:x:65533:\ndocker-hosting:x:%s:\n' "$hgid" > "$XTK_SSH/group"
# shadow: create with system users locked, PRESERVING existing site-user passwords
[ -f "$XTK_SSH/shadow" ] || { printf 'root:!:20000::::::\nsshd:!:20000::::::\nnobody:!:20000::::::\n' > "$XTK_SSH/shadow"; chmod 600 "$XTK_SSH/shadow"; }
(cd "$XTK_SSH" && docker compose -p xtk-sshd up -d --build) >/dev/null 2>&1
echo "sshd gateway up on :2222"
