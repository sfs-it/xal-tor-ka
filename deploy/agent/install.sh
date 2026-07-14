#!/usr/bin/env bash
# xtk-agent installer — installa l'agente hosting PRIVILEGIATO di Xal-Tor-Ka su QUESTO host.
#
# Va lanciato come root SUL HOST — MAI dal container gatekeeper (che è deliberatamente
# non-privilegiato e pilota questo agente via un socket unix vettato). L'agente gira come
# root perché deve creare utenti-OS, lanciare docker e gestire /opt/sites; la sicurezza
# viene dal MODELLO STRETTO (insieme fisso di script root-owned vettati + parametri
# validati, non iniettabili), non dal sandboxing.
#
# Idempotente: rilanciarlo aggiorna binario/comandi/templates e riavvia l'agente.
#
# Uso:
#   sudo deploy/agent/install.sh [--socket-gid GID] [--overlay] [--dev] [--repo DIR]
#     --socket-gid GID   gid del socket 0660, così il container UI-hosting può connettersi
#                        (default 1997; deve combaciare con XTK_AGENT_GID nell'overlay)
#     --overlay          crea anche la rete xtk-hosting + alza l'overlay compose del modulo
#                        (va lanciato da un checkout del repo)
#     --dev              comodità sandbox: implica --overlay + avvisi di mutazione-host
#     --repo DIR         root del repo (default: derivato dalla posizione di questo script)
set -euo pipefail

GID=1997 ; OVERLAY=0 ; DEV=0
SELF="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$SELF/../.." && pwd)"
while [ $# -gt 0 ]; do case "$1" in
  --socket-gid) GID="$2"; shift 2;;
  --overlay)    OVERLAY=1; shift;;
  --dev)        DEV=1; OVERLAY=1; shift;;
  --repo)       REPO="$2"; shift 2;;
  -h|--help)    sed -n '2,22p' "$0"; exit 0;;
  *) echo "opzione sconosciuta: $1" >&2; exit 2;;
esac; done

[ "$(id -u)" = 0 ] || { echo "errore: lanciami come root (sudo)"; exit 1; }
[ -d "$REPO/agent/xtk-agent" ] || { echo "errore: repo non trovato in $REPO (usa --repo)"; exit 1; }
log(){ printf '\n\033[1m== %s\033[0m\n' "$*"; }

[ "$DEV" = 1 ] && cat <<'EOF'
[--dev] Questo installer MUTA il host: crea un demone root (xtk-agent), un gruppo di
sistema, e (con l'agente attivo) creerà utenti-OS e docker reali. Usalo su un box
sandbox/dev — non su una macchina che vuoi tenere pulita.
EOF

log "1/6 build del binario agente"
if command -v go >/dev/null 2>&1; then
  ( cd "$REPO" && go build -o "/tmp/xtk-agent.$$" ./agent/xtk-agent )
  install -m 0755 "/tmp/xtk-agent.$$" /usr/local/bin/xtk-agent && rm -f "/tmp/xtk-agent.$$"
  echo "  installato /usr/local/bin/xtk-agent"
elif [ -x /usr/local/bin/xtk-agent ]; then
  echo "  'go' assente: tengo il binario esistente /usr/local/bin/xtk-agent"
else
  echo "errore: 'go' assente e nessun /usr/local/bin/xtk-agent prebuilt" >&2; exit 1
fi

log "2/6 gruppo del socket (gid $GID)"
if getent group xtk-agent >/dev/null; then
  echo "  gruppo xtk-agent già presente: $(getent group xtk-agent)"
else
  groupadd -g "$GID" xtk-agent && echo "  creato gruppo xtk-agent (gid $GID)"
fi

log "3/6 comandi + templates vettati (root:root)"
install -d /usr/local/lib/xtk-agent
cp -a "$REPO/agent/commands"  /usr/local/lib/xtk-agent/
cp -a "$REPO/agent/templates" /usr/local/lib/xtk-agent/
chown -R root:root /usr/local/lib/xtk-agent          # GUARDRAIL: l'agente rifiuta script non-root
chmod +x /usr/local/lib/xtk-agent/commands/*.sh
cmds=(/usr/local/lib/xtk-agent/commands/*.sh)
echo "  ${#cmds[@]} comandi · templates: $(printf '%s ' /usr/local/lib/xtk-agent/templates/*/ | xargs -n1 basename 2>/dev/null | tr '\n' ' ')"

log "4/6 unit systemd (con --socket-gid $GID)"
unit=/etc/systemd/system/xtk-agent.service
install -m 0644 "$REPO/deploy/agent/xtk-agent.service" "$unit"
sed -i "s|^  --trusted-uid 0\$|  --trusted-uid 0 --socket-gid $GID|" "$unit"   # unit fresca → sempre appende
systemctl daemon-reload
systemctl enable --now xtk-agent
echo "  agente: $(systemctl is-active xtk-agent) · $unit"

log "5/6 verifica socket"
for _ in 1 2 3 4 5; do [ -S /run/xtk-agent/agent.sock ] && break; sleep 1; done
ls -l /run/xtk-agent/agent.sock 2>&1 || { echo "  socket non pronto → journalctl -u xtk-agent -n50"; exit 1; }

if [ "$OVERLAY" = 1 ]; then
  log "6/6 modulo hosting (rete + overlay compose)"
  docker network inspect xtk-hosting >/dev/null 2>&1 || docker network create xtk-hosting
  ( cd "$REPO" && XTK_AGENT_GID="$GID" docker compose -f docker-compose.yml -f ext/hosting/docker-compose.yml up -d --build )
  echo "  UI hosting su · il core espone /admin/hosting e mostra la voce di menù Hosting"
else
  log "6/6 modulo hosting: SALTATO (--overlay per alzarlo)"
  echo "  poi, dal repo:"
  echo "    docker network create xtk-hosting"
  echo "    XTK_AGENT_GID=$GID docker compose -f docker-compose.yml -f ext/hosting/docker-compose.yml up -d --build"
fi

log "FATTO"
echo "Agente: $(systemctl is-active xtk-agent) · socket /run/xtk-agent/agent.sock (gruppo gid $GID)"
[ "$OVERLAY" = 1 ] && echo "Ricarica /admin: la voce di menù «Hosting» ora è attiva."
