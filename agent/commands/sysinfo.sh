#!/bin/bash
# Vetted command: no untrusted input. Params (none here) arrive only as XTK_P_* env.
set -euo pipefail
echo "host:    $(hostname)"
echo "uptime:  $(uptime -p 2>/dev/null || true)"
echo "kernel:  $(uname -r)"
echo "docker:  $(docker ps --format '{{.Names}}' 2>/dev/null | wc -l) running container(s)"
echo "disk:    $(df -h / | awk 'NR==2{print $4" free of "$2}')"
