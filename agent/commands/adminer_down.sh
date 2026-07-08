#!/bin/bash
set -euo pipefail
token="${XTK_P_TOKEN:?}"; [[ "$token" =~ ^[a-f0-9]{8,40}$ ]] || { echo "invalid token" >&2; exit 2; }
docker rm -f "xtk-adminer-$token" >/dev/null 2>&1 || true
echo "adminer $token down"
