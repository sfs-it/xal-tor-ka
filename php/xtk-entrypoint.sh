#!/bin/sh
# Xal-Tor-Ka php entrypoint — materializza a caldo i moduli VARIABILI scelti dall'admin,
# sull'immagine STANDARD (niente rebuild per-vhost). La lista arriva come env XTK_PHP_EXT
# (allow-listata a monte dall'agente: mai testo libero). Il set COMUNE è già cotto
# nell'immagine; qui si installano solo gli EXTRA non ancora presenti.
set -e
if [ -n "${XTK_PHP_EXT:-}" ]; then
  present="$(php -m 2>/dev/null | tr '[:upper:]' '[:lower:]')"
  for m in $(echo "$XTK_PHP_EXT" | tr ',' ' '); do
    [ -z "$m" ] && continue
    ml="$(echo "$m" | tr '[:upper:]' '[:lower:]')"
    if echo "$present" | grep -qx "$ml"; then
      continue   # già presente (cotto nell'immagine, o installato in un boot precedente)
    fi
    echo "xtk-php: installo modulo '$m'…" >&2
    if install-php-extensions "$m" >&2; then
      echo "xtk-php: modulo '$m' OK" >&2
    else
      echo "xtk-php: modulo '$m' FALLITO — proseguo senza" >&2
    fi
  done
fi
exec docker-php-entrypoint "$@"
