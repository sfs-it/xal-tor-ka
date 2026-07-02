#!/bin/sh
# Avvia NGINX e lo ricarica quando cambia la conf.d (backends.conf rigenerato dal
# proxy manager). Usa POLLING sull'hash del contenuto invece di inotify: gli
# eventi inotify sui bind mount di Docker Desktop / WSL2 non sono consegnati in
# modo affidabile tra container, mentre la *lettura* del file riflette sempre il
# contenuto corrente. Ricarica solo se `nginx -t` passa (altrimenti mantiene la
# configurazione corrente: nessun reload emesso).
set -eu

nginx -g 'daemon off;' &
NGINX_PID=$!

# Hash conf.d AND the per-host certificates: a cert (re)issue/renewal replaces
# <host>.crt without changing conf.d, so certs must be watched too or NGINX would
# keep serving the old certificate. The .well-known challenge files live in a
# subdir and are not matched by *.crt, so issuance doesn't cause reload churn.
confhash() { { cat /etc/nginx/conf.d/*.conf; cat /etc/nginx/certs/*.crt; } 2>/dev/null | md5sum | cut -d' ' -f1; }

last="$(confhash)"
(
  while sleep 2; do
    cur="$(confhash)"
    [ "$cur" = "$last" ] && continue
    last="$cur"
    if nginx -t 2>/tmp/nginx-t.log; then
      nginx -s reload && echo "[xtk-reload] applicata nuova configurazione"
    else
      echo "[xtk-reload] nginx -t FALLITO, mantengo la configurazione corrente" >&2
      cat /tmp/nginx-t.log >&2
    fi
  done
) &

wait "$NGINX_PID"
