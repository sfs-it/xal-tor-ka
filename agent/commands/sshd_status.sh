#!/bin/bash
set -euo pipefail
: "${XTK_SSH:=/opt/xtk-ssh}"
installed=false; running=false
if [ -f "$XTK_SSH/docker-compose.yml" ]; then
  installed=true
  cid="$(cd "$XTK_SSH" && docker compose -p xtk-sshd ps -q sshd 2>/dev/null || true)"
  [ -n "$cid" ] && [ "$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null)" = true ] && running=true
fi
printf '{"installed":%s,"running":%s,"port":2222}\n' "$installed" "$running"
