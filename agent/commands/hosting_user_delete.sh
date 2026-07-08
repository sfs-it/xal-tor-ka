#!/bin/bash
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
user="site-$name"
[ -d "$XTK_SITES/$name" ] && { echo "not orphaned: site '$name' still exists — use site destroy" >&2; exit 3; }
id "$user" >/dev/null 2>&1 || { echo "no such user: $user" >&2; exit 3; }
userdel "$user"
echo "deleted orphan user $user"
