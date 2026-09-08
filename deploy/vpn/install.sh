#!/usr/bin/env bash
# xtk-vpn installer — installa il modulo VPN di Xal-Tor-Ka su QUESTO nodo (node-agnostic).
#
# Gemello di deploy/agent/install.sh, ma per il modulo VPN. Va lanciato come root SUL
# NODO (hydra, xaltorka1, indifferente: nessun IP/hostname è cablato — tutto il
# node-specific sta in vpn.json / env, mai qui). Idempotente.
#
# FASI: F1 (questo) = alza SOLO la UI (xtk-vpn-ui), inerte finché vpn.json ha
# enabled=false → nessun tunnel, nessun effetto di rete. F2 aggiungerà qui l'install del
# vpn-agent vettato (binario + gruppo-socket gid + systemd + comandi root:root) e il
# data-plane privilegiato — vedi il blocco marcato "# F2" in fondo.
#
# Uso:
#   sudo deploy/vpn/install.sh [--overlay] [--dev] [--repo DIR] [--socket-gid GID]
#     --overlay      rende sticky il COMPOSE_FILE (MERGE con hosting se presente) e alza
#                    l'overlay compose del modulo (va lanciato da un checkout del repo)
#     --dev          comodità sandbox: implica --overlay
#     --repo DIR     root del repo (default: derivato dalla posizione di questo script)
#     --socket-gid   gid del socket del vpn-agent (default 1998; usato da F2)
set -euo pipefail

GID=1998 ; OVERLAY=0 ; DEV=0
SELF="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$SELF/../.." && pwd)"
while [ $# -gt 0 ]; do case "$1" in
  --socket-gid) GID="$2"; shift 2;;
  --overlay)    OVERLAY=1; shift;;
  --dev)        DEV=1; OVERLAY=1; shift;;
  --repo)       REPO="$2"; shift 2;;
  -h|--help)    sed -n '2,24p' "$0"; exit 0;;
  *) echo "opzione sconosciuta: $1" >&2; exit 2;;
esac; done

[ "$(id -u)" = 0 ] || { echo "errore: lanciami come root (sudo)"; exit 1; }
[ -f "$REPO/ext/vpn/docker-compose.yml" ] || { echo "errore: repo non trovato in $REPO (usa --repo)"; exit 1; }
log(){ printf '\n\033[1m== %s\033[0m\n' "$*"; }

OVERLAY_PATH="ext/vpn/docker-compose.yml"

log "1/3 config del modulo (vpn.json)"
# Il modulo POSSIEDE il suo vpn.json (fuori dal core config.json). Lo mettiamo in una DIR
# montata :rw (data/vpn) — dir, non file, così il save atomico resta inode-stabile. Seed
# dall'esempio se assente (default inerte enabled=false); non sovrascrive uno esistente.
install -d "$REPO/data/vpn"
if [ -f "$REPO/data/vpn/vpn.json" ]; then
  echo "  vpn.json già presente — non tocco (config del nodo)"
else
  install -m 0644 "$REPO/ext/vpn/vpn.json.example" "$REPO/data/vpn/vpn.json"
  echo "  vpn.json seminato dall'esempio (enabled=false, inerte)"
fi
# La UI gira come uid 65532 (distroless nonroot) e da F3 SCRIVE vpn.json → la dir dev'essere
# sua. Chown numerico (l'uid può non avere nome sul nodo).
chown -R 65532:65532 "$REPO/data/vpn" 2>/dev/null || echo "  (chown data/vpn saltato: non-root fs?)"

if [ "$OVERLAY" != 1 ]; then
  log "2/3 overlay: SALTATO (--overlay per alzarlo)"
  echo "  poi, dal repo, rendendo sticky il COMPOSE_FILE (MERGE, non sovrascrivere hosting):"
  echo "    # aggiungi ':$OVERLAY_PATH' alla riga COMPOSE_FILE del .env, poi:"
  echo "    XTK_VPN_AGENT_GID=$GID docker compose up -d --build"
  log "FATTO (solo config)"; exit 0
fi

log "2/3 COMPOSE_FILE sticky (MERGE dell'overlay VPN)"
# ⚠️ MERGE, non append-cieco: con hosting già presente, COMPOSE_FILE esiste già → un
# semplice 'append-se-manca-la-riga' NON aggiungerebbe la VPN (regressione silente, gemella
# dell'incidente 2026-07-17). Qui fondiamo l'overlay nella LISTA esistente, idempotente.
env_file="$REPO/.env"; touch "$env_file"
if grep -q '^COMPOSE_FILE=' "$env_file"; then
  cur="$(grep '^COMPOSE_FILE=' "$env_file" | head -1 | cut -d= -f2-)"
  case ":$cur:" in
    *":$OVERLAY_PATH:"*) newv="$cur"; echo "  overlay VPN già nella lista COMPOSE_FILE";;
    *) newv="$cur:$OVERLAY_PATH"; echo "  overlay VPN aggiunto alla lista esistente (merge)";;
  esac
  sed -i "s|^COMPOSE_FILE=.*|COMPOSE_FILE=$newv|" "$env_file"
else
  printf 'COMPOSE_FILE=docker-compose.yml:%s\n' "$OVERLAY_PATH" >> "$env_file"
  echo "  COMPOSE_FILE creato (base + overlay VPN)"
fi
echo "  COMPOSE_FILE=$(grep '^COMPOSE_FILE=' "$env_file" | head -1 | cut -d= -f2-)"

log "3/3 up del modulo (build + start; usa COMPOSE_FILE dal .env)"
( cd "$REPO" && XTK_VPN_AGENT_GID="$GID" docker compose up -d --build )
# nginx per ultimo: alzare l'overlay ricrea il core → nuovo IP docker → nginx cachea il
# vecchio → 502. Un restart mirato di nginx ri-risolve (gemello di 50-hosting.sh).
( cd "$REPO" && docker compose restart nginx >/dev/null 2>&1 || true )

log "verifica"
for _ in 1 2 3 4 5; do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1/healthz 2>/dev/null || true)"
  [ "$code" = "200" ] && break; sleep 1
done
echo "  core /healthz → ${code:-n/a}"
echo "  (il modulo è INERTE finché vpn.json ha enabled=false: /admin/vpn mostra solo lo stato)"

log "FATTO"
echo "Modulo VPN su (UI xtk-vpn-ui:8091). Ricarica /admin: la voce «VPN» ora è attiva."
echo "F2 aggiungerà qui: build del vpn-agent vettato + gruppo-socket gid $GID + systemd + data-plane."
exit 0

# ---------------------------------------------------------------------------------------
# F2 (da implementare qui, gemello di deploy/agent/install.sh 1/6..5/6):
#   - build binario vpn-agent (host `go build`, fallback container golang:1.25 se manca go)
#   - groupadd -g $GID xtk-vpn-agent
#   - install comandi vettati in /usr/local/lib/xtk-vpn-agent/commands (root:root, 0755 ESATTO)
#   - unit systemd deploy/vpn/xtk-vpn-agent.service (--socket-gid $GID, socket /run/xtk-vpn-agent)
#   - verifica socket /run/xtk-vpn-agent/agent.sock
# ---------------------------------------------------------------------------------------
