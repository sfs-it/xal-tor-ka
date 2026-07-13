#!/bin/bash
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"
name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
f="$XTK_SITES/$name/.vhosts/$vhost/db.env"
[ -f "$f" ] && grep -E '^DB_[A-Z]+=' "$f" || true
