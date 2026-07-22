#!/bin/bash
# vhost_php_ext — imposta i moduli PHP à-la-carte di un vhost. I moduli richiesti arrivano
# SOLO come env var (XTK_P_MODULES), separati da virgola, e vengono verificati contro una
# ALLOW-LIST rigida qui dentro (il confine di sicurezza — mai testo libero, mai una shell).
# La lista viene scritta in .xtk-stack (php_extensions=) e iniettata come XTK_PHP_EXT nel
# db.env del vhost; il container php viene ricreato così l'entrypoint di xtk-php li
# materializza al boot. Richiede l'immagine xtk-php (template php-fpm/laravel). root:root.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"

# --- ALLOW-LIST (autoritativa; tenere in sync con agent/php-modules.allow usata dalla UI) ---
ALLOW="redis igbinary imagick soap ldap gmp exif sockets xsl mongodb apcu memcached amqp"

name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"; modules="${XTK_P_MODULES:-}"
vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
dir="$XTK_SITES/$name"; vdir="$dir/.vhosts/$vhost"
f="$vdir/docker-compose.yml"; stack="$vdir/.xtk-stack"; denv="$vdir/db.env"
[ -f "$f" ] || { echo "no such vhost: $name/$vhost" >&2; exit 3; }

# normalizza + valida ogni modulo richiesto contro l'allow-list; dedup mantenendo l'ordine
clean=""
IFS=',' read -ra reqs <<< "$modules"
for m in "${reqs[@]:-}"; do
  m="$(printf '%s' "$m" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
  [ -z "$m" ] && continue
  case "$m" in *[!a-z0-9_]*) echo "invalid module name: $m" >&2; exit 2 ;; esac
  ok=0; for a in $ALLOW; do [ "$m" = "$a" ] && { ok=1; break; }; done
  [ "$ok" = 1 ] || { echo "module not in allow-list: $m" >&2; exit 2; }
  case ",$clean," in *",$m,"*) : ;; *) clean="${clean:+$clean,}$m" ;; esac
done

# richiede l'immagine xtk-php (materializzatore) — altrimenti i moduli non verrebbero caricati
if ! grep -qE 'image:[[:space:]]*xtk-php:' "$f"; then
  echo "immagine php del vhost non e xtk-php (nessun materializzatore a-la-carte) — ricrea il vhost su php-fpm/laravel" >&2
  exit 4
fi

# .xtk-stack: sostituisce la riga php_extensions= (la aggiunge se assente)
own_stack="$(stat -c '%u:%g' "$stack" 2>/dev/null || echo 0:0)"
tmp="$(mktemp)"; grep -v '^php_extensions=' "$stack" 2>/dev/null > "$tmp" || true
printf 'php_extensions=%s\n' "$clean" >> "$tmp"; cat "$tmp" > "$stack"; rm -f "$tmp"
chown "$own_stack" "$stack" 2>/dev/null || true

# db.env (env_file del php): sostituisce la riga XTK_PHP_EXT= (la aggiunge se assente)
own_denv="$(stat -c '%u:%g' "$denv" 2>/dev/null || echo 0:0)"
tmp="$(mktemp)"; grep -v '^XTK_PHP_EXT=' "$denv" 2>/dev/null > "$tmp" || true
printf 'XTK_PHP_EXT=%s\n' "$clean" >> "$tmp"; cat "$tmp" > "$denv"; rm -f "$tmp"
chown "$own_denv" "$denv" 2>/dev/null || true

# ricrea il php così l'entrypoint ri-materializza i moduli
proj="$(vhost_project "$name" "$vhost")"
docker compose --project-directory "$dir" -f "$f" -p "$proj" up -d --force-recreate php \
  >/tmp/xtk_phpext.out 2>&1 || { echo "moduli salvati, ma il recreate e fallito:" >&2; tail -c 1500 /tmp/xtk_phpext.out >&2; exit 5; }
echo "moduli php per $name/$vhost: [${clean:-nessuno}]"
