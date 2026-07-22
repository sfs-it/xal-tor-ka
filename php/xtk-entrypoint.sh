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

# --- CONFIG PHP (impostazioni variabili scelte dall'admin, materializzate come drop-in conf.d
# sull'immagine STANDARD; il contenuto arriva base64 dall'agente, allow-listato/validato a monte;
# qui si SCRIVE come file, mai eval). php.ini: drop-in globale (prefisso zz- → vince sui default).
# php-fpm: direttive nel pool [www], validate con `php-fpm -t` (se rompono, si rimuove il drop-in
# così il container parte comunque). ---
ini_dropin="/usr/local/etc/php/conf.d/zz-xtk-custom.ini"
fpm_dropin="/usr/local/etc/php-fpm.d/zz-xtk-custom.conf"
rm -f "$ini_dropin" "$fpm_dropin"
if [ -n "${XTK_PHP_INI_B64:-}" ]; then
  if printf '%s' "$XTK_PHP_INI_B64" | base64 -d > "$ini_dropin" 2>/dev/null && [ -s "$ini_dropin" ]; then
    echo "xtk-php: config php.ini applicata (zz-xtk-custom.ini)" >&2
  else
    rm -f "$ini_dropin"
  fi
fi
if [ -n "${XTK_PHP_FPM_B64:-}" ]; then
  { echo "[www]"; printf '%s' "$XTK_PHP_FPM_B64" | base64 -d 2>/dev/null; } > "$fpm_dropin"
  if [ -s "$fpm_dropin" ] && php-fpm -t >/dev/null 2>&1; then
    echo "xtk-php: config php-fpm applicata (zz-xtk-custom.conf)" >&2
  else
    echo "xtk-php: config php-fpm INVALIDA (php-fpm -t) — drop-in rimosso, proseguo" >&2
    rm -f "$fpm_dropin"
  fi
fi

exec docker-php-entrypoint "$@"
