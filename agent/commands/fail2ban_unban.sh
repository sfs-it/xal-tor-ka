#!/bin/bash
# Unban a single IP from the 'xaltorka' jail. Param: ip. Runs as root.
set -euo pipefail
IP="${XTK_P_IP:-}"
if [ -z "$IP" ]; then printf '{"ok":false,"error":"ip required"}\n'; exit 1; fi
if ! command -v fail2ban-client >/dev/null 2>&1; then printf '{"ok":false,"error":"fail2ban not installed"}\n'; exit 1; fi

if fail2ban-client set xaltorka unbanip "$IP" >/dev/null 2>&1; then
  printf '{"ok":true,"unbanned":"%s"}\n' "$IP"
else
  printf '{"ok":false,"error":"unban failed (ip not banned, or jail absent)","ip":"%s"}\n' "$IP"
fi
