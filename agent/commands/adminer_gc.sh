#!/bin/bash
set -euo pipefail
ids="$(docker ps -aq --filter label=xtk-adminer=1 || true)"
[ -n "$ids" ] && docker rm -f $ids >/dev/null 2>&1 || true
echo "adminer gc done"
