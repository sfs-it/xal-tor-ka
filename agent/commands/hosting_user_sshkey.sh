#!/bin/bash
# Generate an ed25519 keypair for the site user, install the PUBLIC key into its
# chroot ~/.ssh/authorized_keys (root-owned → passes sshd StrictModes), refresh the
# SSH gateway roster so sshd knows the user, and print the PRIVATE key ONCE (not
# stored; audit logs only param keys). Key auth needs no password.
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"; : "${XTK_SSH:=/opt/xtk-ssh}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
user="site-$name"; dir="$XTK_SITES/$name"; [ -d "$dir" ] || { echo "no such site: $name" >&2; exit 3; }
[ -d "$XTK_SSH" ] || { echo "SCP/SFTP gateway not installed — bring it up first" >&2; exit 4; }
pass="${XTK_P_PASSPHRASE-}"                # optional private-key passphrase
comment="${XTK_P_COMMENT:-$user@xaltorka}" # optional public-key comment
uid="$(id -u "$user")"; gid="$(id -g "$user")"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
# values are single quoted args to ssh-keygen — no shell injection
ssh-keygen -t ed25519 -f "$tmp/id" -N "$pass" -C "$comment" -q
install -d -m0700 "$dir/.ssh"
cat "$tmp/id.pub" >> "$dir/.ssh/authorized_keys"   # append (keep existing keys)
# sshd reads authorized_keys AS the user (after privsep) → it must be user-owned;
# the home stays root-owned for the chroot, which StrictModes accepts.
chown -R "$uid:$gid" "$dir/.ssh"; chmod 0600 "$dir/.ssh/authorized_keys"
# refresh the synthesized passwd so a site created after sshd_up is known to sshd
hgid="$(getent group docker-hosting | cut -d: -f3)"
{ printf 'root:x:0:0:root:/root:/sbin/nologin\nsshd:x:22:22:sshd:/dev/null:/sbin/nologin\nnobody:x:65534:65534:nobody:/:/sbin/nologin\n'
  getent passwd | awk -F: -v g="$hgid" '$4==g {print $1":x:"$3":"$4":"$5":"$6":/sbin/nologin"}'; } > "$XTK_SSH/passwd"
# ensure a shadow entry so the account is valid for pubkey; '*' = no password login,
# NOT locked (leave an existing password hash untouched).
grep -q "^$user:" "$XTK_SSH/shadow" 2>/dev/null || { printf '%s:*:20000:0:99999:7:::\n' "$user" >> "$XTK_SSH/shadow"; chmod 600 "$XTK_SSH/shadow"; }
cat "$tmp/id"; cat "$tmp/id.pub"
