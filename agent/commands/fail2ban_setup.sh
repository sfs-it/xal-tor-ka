#!/bin/bash
# Install fail2ban and (re)configure the 'xaltorka' jail: ban IPs that brute-force
# the gate, read from its stable auth-failure log (audit.go). Layer 2 of the defence
# (RAM bruteforce in the gate = layer 1; IP-ban at the host firewall = layer 2).
#
# DOCKER-AWARE: the gate is a published container port, so traffic is DNAT'd
# (prerouting -> forward), NOT delivered to INPUT. A stock fail2ban INPUT drop would
# look active but silently miss it. So we drop from a custom nftables set at PREROUTING
# (priority -300, before docker's dstnat). The ruleset lives in a .nft file loaded with
# a single `nft -f` (robust: no fragile inline braces in the fail2ban action); fail2ban
# only adds/removes the set's elements.
#
# ANTI-LOCKOUT: ignoreip (admin IPs + the LAN subnet) is REQUIRED and always includes
# loopback. Bans hit only tcp dport {80,443}, never SSH. Runs as root. Debian/apt + nft.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

IGNOREIP="${XTK_P_IGNOREIP:-}"
LOGPATH="${XTK_P_LOGPATH:-/opt/xaltorka/logs/auth.log}"
MAXRETRY="${XTK_P_MAXRETRY:-5}"
FINDTIME="${XTK_P_FINDTIME:-600}"
BANTIME="${XTK_P_BANTIME:-3600}"

if [ -z "$IGNOREIP" ]; then
  printf '{"ok":false,"error":"ignoreip is required (anti-lockout: pass admin IPs + LAN subnet)"}\n'; exit 1
fi

if ! command -v fail2ban-client >/dev/null 2>&1; then
  apt-get update -qq >/dev/null 2>&1 || true
  apt-get install -y -qq fail2ban >/dev/null 2>&1 || { printf '{"ok":false,"error":"fail2ban install failed"}\n'; exit 1; }
fi

# nftables ruleset (idempotent: add-then-flush, so re-load is safe). Dropped at
# PREROUTING before docker's dstnat so container-bound brute-force is really blocked.
cat > /etc/xaltorka-f2b.nft <<'NFT'
add table inet f2b-xaltorka
flush table inet f2b-xaltorka
add set inet f2b-xaltorka banned { type ipv4_addr; flags timeout; }
add chain inet f2b-xaltorka pre { type filter hook prerouting priority -300; policy accept; }
add rule inet f2b-xaltorka pre ip saddr @banned tcp dport { 80, 443 } drop
NFT
nft -f /etc/xaltorka-f2b.nft   # load now; also loaded by the action on jail (re)start

# Custom action: load the ruleset (single command) + manage set elements. IPv4 only for
# now (bulk of brute-force); IPv6 is a follow-up.
cat > /etc/fail2ban/action.d/xaltorka-nft.conf <<'ACTION'
[Definition]
actionstart = nft -f /etc/xaltorka-f2b.nft
actionstop  = nft delete table inet f2b-xaltorka
actioncheck = nft list set inet f2b-xaltorka banned
actionban   = nft add element inet f2b-xaltorka banned { <ip> timeout <bantime>s }
actionunban = nft delete element inet f2b-xaltorka banned { <ip> }
[Init]
ACTION

# Filter: matches the stable line "<ts> xaltorka auth-fail ip=<IP> event=<e> <detail>".
cat > /etc/fail2ban/filter.d/xaltorka.conf <<'FILTER'
[Definition]
failregex = ^\s*\S+ xaltorka auth-fail ip=<HOST>\b
datepattern = ^%%Y-%%m-%%dT%%H:%%M:%%S
FILTER

cat > /etc/fail2ban/jail.d/xaltorka.conf <<EOF
# Disable the stock sshd jail: this box logs sshd to the journal (no /var/log/auth.log),
# so the default sshd jail makes fail2ban fail to start. SSH here is key-only anyway.
[sshd]
enabled = false

[xaltorka]
enabled     = true
filter      = xaltorka
logpath     = ${LOGPATH}
backend     = polling
logtimezone = UTC
maxretry    = ${MAXRETRY}
findtime    = ${FINDTIME}
bantime     = ${BANTIME}
banaction   = xaltorka-nft
ignoreip    = 127.0.0.1/8 ::1 ${IGNOREIP}
EOF

systemctl enable fail2ban >/dev/null 2>&1 || true
systemctl restart fail2ban
sleep 2

if ! fail2ban-client status xaltorka >/dev/null 2>&1; then
  err="$(tail -5 /var/log/fail2ban.log 2>/dev/null | tr '\n' ' ' | tr '"' \' )"
  printf '{"ok":false,"error":"jail not active","log":"%s"}\n' "$err"; exit 1
fi
nft_ok=false; nft list table inet f2b-xaltorka >/dev/null 2>&1 && nft_ok=true
banned="$(fail2ban-client status xaltorka 2>/dev/null | sed -n 's/.*Banned IP list:[[:space:]]*//p')"
printf '{"ok":true,"jail":"xaltorka","nft_prerouting":%s,"logpath":"%s","ignoreip":"127.0.0.1/8 ::1 %s","maxretry":%s,"findtime":%s,"bantime":%s,"banned":"%s"}\n' \
  "$nft_ok" "$LOGPATH" "$IGNOREIP" "$MAXRETRY" "$FINDTIME" "$BANTIME" "$banned"
