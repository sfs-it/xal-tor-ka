#!/bin/bash
# The client can only pass log ∈ {enum} and lines ∈ digits (validated by the agent).
# The script — the trust boundary — maps the safe token to the real path.
set -euo pipefail
case "${XTK_P_LOG:-}" in
  nginx-access) f=/var/log/nginx/access.log ;;
  nginx-error)  f=/var/log/nginx/error.log ;;
  syslog)       f=/var/log/syslog ;;
  *) echo "unknown log token" >&2; exit 2 ;;
esac
tail -n "${XTK_P_LINES:-100}" "$f"
