#!/bin/bash
set -euo pipefail
: "${XTK_SITES:=/opt/sites}"
name="${XTK_P_NAME:?}"; [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "invalid name" >&2; exit 2; }
f="$XTK_SITES/$name/db.env"
[ -f "$f" ] && grep -E '^DB_[A-Z]+=' "$f" || true
