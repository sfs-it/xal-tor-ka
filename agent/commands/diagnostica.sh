#!/bin/bash
# diagnostica — collector di osservabilità READ-ONLY. Aggrega eventi recenti da piu sorgenti
# e li classifica per livello (INFO | ALERT | CRITICAL), emettendo un array JSON:
#   [{"ts","level","source","site","msg"}, ...]
# Consumato dalla pagina admin «Log/Criticità» (filtro TUTTO / INFO+ / ALERT+ / CRITICAL+).
# Nessuna mutazione: solo tail/journalctl. root:root, param via env.
set -uo pipefail
source "$(dirname "$0")/_vhost_lib.sh"

N="${XTK_P_LIMIT:-120}"; case "$N" in ''|*[!0-9]*) N=120;; esac; [ "$N" -gt 500 ] && N=500

jesc() { printf '%s' "$1" | tr -d '\r\t' | sed 's/\\/\\\\/g; s/"/\\"/g'; }
lines=""
add() { # ts level source site msg
  local j; j="$(printf '{"ts":"%s","level":"%s","source":"%s","site":"%s","msg":"%s"}' \
    "$(jesc "$1")" "$2" "$3" "$(jesc "$4")" "$(jesc "$5")")"
  lines="${lines:+$lines,}$j"
}

# 1) AGENTE — ogni comando vettato: exit!=0 -> CRITICAL, altrimenti INFO
while IFS= read -r line; do
  case "$line" in *msg=*) ;; *) continue;; esac
  ex="$(printf '%s' "$line" | sed -n 's/.*exit=\([0-9]\{1,\}\).*/\1/p')"
  cmd="$(printf '%s' "$line" | sed -n 's/.*cmd=\([a-z_]\{1,\}\).*/\1/p')"
  ts="$(printf '%s' "$line" | sed -n 's/.*time=\([0-9T:.+-]\{1,\}\).*/\1/p')"
  lvl=INFO; { [ -n "$ex" ] && [ "$ex" != 0 ]; } && lvl=CRITICAL
  add "${ts:-}" "$lvl" agent "" "cmd=${cmd:-?} exit=${ex:-?}"
done < <(journalctl -u xtk-agent -n "$N" --no-pager -o cat 2>/dev/null || true)

# 2) NGINX per-vhost access.log — status 5xx -> CRITICAL, 4xx -> ALERT (2xx/3xx ignorati)
for al in "$XTK_SITES"/*/logs/*/access.log; do
  [ -f "$al" ] || continue
  site="$(printf '%s' "$al" | sed -E 's#.*/sites/([^/]+)/logs/([^/]+)/access.log#\1/\2#')"
  while IFS= read -r c meth path; do
    case "$c" in ''|*[!0-9]*) continue;; esac
    if [ "$c" -ge 500 ]; then lvl=CRITICAL; elif [ "$c" -ge 400 ]; then lvl=ALERT; else continue; fi
    add "" "$lvl" nginx "$site" "$meth $path -> $c"
  done < <(tail -n "$N" "$al" 2>/dev/null | awk '{print $9" "$6" "$7}' | tr -d '"')
done

# 3) NGINX per-vhost error.log — [error]/[crit]/[alert] -> CRITICAL, [warn] -> ALERT
for el in "$XTK_SITES"/*/logs/*/error.log; do
  [ -f "$el" ] || continue
  site="$(printf '%s' "$el" | sed -E 's#.*/sites/([^/]+)/logs/([^/]+)/error.log#\1/\2#')"
  while IFS= read -r line; do
    case "$line" in *'[error]'*|*'[crit]'*|*'[alert]'*) lvl=CRITICAL;; *'[warn]'*) lvl=ALERT;; *) continue;; esac
    add "$(printf '%s' "$line" | awk '{print $1" "$2}')" "$lvl" nginx "$site" "$(printf '%s' "$line" | cut -c1-160)"
  done < <(tail -n "$N" "$el" 2>/dev/null)
done

# 4) AUTH del gate — deny/fail/invalid/forbidden -> ALERT
if [ -f /opt/xaltorka/logs/auth.log ]; then
  while IFS= read -r line; do
    case "$line" in *deny*|*denied*|*fail*|*invalid*|*forbidden*)
      add "" ALERT auth "" "$(printf '%s' "$line" | cut -c1-160)";; esac
  done < <(tail -n "$N" /opt/xaltorka/logs/auth.log 2>/dev/null)
fi

printf '[%s]\n' "$lines"
