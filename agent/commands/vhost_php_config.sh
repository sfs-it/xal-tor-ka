#!/bin/bash
# vhost_php_config — imposta la CONFIG PHP di un vhost (estende l'à-la-carte dai *moduli* alle
# *impostazioni*). Le impostazioni comuni (upload_max_filesize, post_max_size, memory_limit,
# max_file_uploads, max_execution_time) arrivano come env var validate; i due frame liberi
# (direttive php.ini e direttive di pool php-fpm) arrivano come base64 (XTK_P_INI_RAW_B64 /
# XTK_P_FPM_RAW_B64) — mai testo libero passato a una shell, mai eval: si decodifica e si
# SCRIVE come file. Storage in .xtk-stack (per l'UI) + db.env (XTK_PHP_INI_B64/XTK_PHP_FPM_B64,
# ciò che l'entrypoint di xtk-php materializza in conf.d al boot). Richiede l'immagine xtk-php.
# root:root.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"

name="${XTK_P_NAME:?}"; vhost="${XTK_P_VHOST:?}"
umf="${XTK_P_UPLOAD_MAX_FILESIZE:-}"; pms="${XTK_P_POST_MAX_SIZE:-}"
mem="${XTK_P_MEMORY_LIMIT:-}"; mfu="${XTK_P_MAX_FILE_UPLOADS:-}"; met="${XTK_P_MAX_EXECUTION_TIME:-}"
ini_b64="${XTK_P_INI_RAW_B64:-}"; fpm_b64="${XTK_P_FPM_RAW_B64:-}"

vhost_valid_name "$name"  || { echo "invalid name"  >&2; exit 2; }
vhost_valid_name "$vhost" || { echo "invalid vhost" >&2; exit 2; }
dir="$XTK_SITES/$name"; vdir="$dir/.vhosts/$vhost"
f="$vdir/docker-compose.yml"; stack="$vdir/.xtk-stack"; denv="$vdir/db.env"
[ -f "$f" ] || { echo "no such vhost: $name/$vhost" >&2; exit 3; }

# richiede l'immagine xtk-php (materializzatore) — altrimenti la config non verrebbe caricata
if ! grep -qE 'image:[[:space:]]*xtk-php:' "$f"; then
  echo "immagine php del vhost non e xtk-php (config PHP non supportata qui) — ricrea il vhost su stack php-fpm o laravel" >&2
  exit 4
fi

# --- validazione difensiva delle impostazioni comuni (il manifest gia le valida; ricontrolliamo) ---
sz='^[0-9]{1,6}[KMG]?$'; int='^[0-9]{1,5}$'
[ -z "$umf" ] || [[ "$umf" =~ $sz ]]  || { echo "invalid upload_max_filesize" >&2; exit 2; }
[ -z "$pms" ] || [[ "$pms" =~ $sz ]]  || { echo "invalid post_max_size" >&2; exit 2; }
[ -z "$mem" ] || [ "$mem" = "-1" ] || [[ "$mem" =~ $sz ]] || { echo "invalid memory_limit" >&2; exit 2; }
[ -z "$mfu" ] || [[ "$mfu" =~ $int ]] || { echo "invalid max_file_uploads" >&2; exit 2; }
[ -z "$met" ] || [[ "$met" =~ $int ]] || { echo "invalid max_execution_time" >&2; exit 2; }

# --- decodifica i frame liberi (base64 → testo; MAI eval, solo scrittura su file) ---
decode_b64() { [ -z "$1" ] && return 0; printf '%s' "$1" | base64 -d 2>/dev/null || { echo "base64 non valido" >&2; exit 2; }; }
ini_raw="$(decode_b64 "$ini_b64")"; fpm_raw="$(decode_b64 "$fpm_b64")"

# --- costruisce il drop-in php.ini: impostazioni comuni (solo le non vuote) + frame libero ---
ini_out=""
add_ini() { [ -n "$2" ] && ini_out+="$1 = $2"$'\n'; }
add_ini upload_max_filesize "$umf"
add_ini post_max_size       "$pms"
add_ini memory_limit        "$mem"
add_ini max_file_uploads    "$mfu"
add_ini max_execution_time  "$met"
[ -n "$ini_raw" ] && ini_out+="$ini_raw"$'\n'

# --- persistenza in .xtk-stack (per il pre-fill dell'UI) ---
own_stack="$(stat -c '%u:%g' "$stack" 2>/dev/null || echo 0:0)"
tmp="$(mktemp)"
grep -vE '^php_(upload_max_filesize|post_max_size|memory_limit|max_file_uploads|max_execution_time|ini_raw_b64|fpm_raw_b64)=' "$stack" 2>/dev/null > "$tmp" || true
{
  printf 'php_upload_max_filesize=%s\n' "$umf"
  printf 'php_post_max_size=%s\n' "$pms"
  printf 'php_memory_limit=%s\n' "$mem"
  printf 'php_max_file_uploads=%s\n' "$mfu"
  printf 'php_max_execution_time=%s\n' "$met"
  printf 'php_ini_raw_b64=%s\n' "$ini_b64"
  printf 'php_fpm_raw_b64=%s\n' "$fpm_b64"
} >> "$tmp"
cat "$tmp" > "$stack"; rm -f "$tmp"; chown "$own_stack" "$stack" 2>/dev/null || true

# --- db.env: ciò che l'entrypoint materializza (ini finale + pool fpm), in base64 single-line ---
ini_final_b64="$(printf '%s' "$ini_out" | base64 -w0)"
own_denv="$(stat -c '%u:%g' "$denv" 2>/dev/null || echo 0:0)"
tmp="$(mktemp)"
grep -vE '^(XTK_PHP_INI_B64|XTK_PHP_FPM_B64)=' "$denv" 2>/dev/null > "$tmp" || true
printf 'XTK_PHP_INI_B64=%s\n' "$ini_final_b64" >> "$tmp"
printf 'XTK_PHP_FPM_B64=%s\n' "$fpm_b64" >> "$tmp"
cat "$tmp" > "$denv"; rm -f "$tmp"; chown "$own_denv" "$denv" 2>/dev/null || true

# --- ricrea il php così l'entrypoint ri-materializza i drop-in conf.d ---
proj="$(vhost_project "$name" "$vhost")"
docker compose --project-directory "$dir" -f "$f" -p "$proj" up -d --force-recreate php \
  >/tmp/xtk_phpcfg.out 2>&1 || { echo "config salvata, ma il recreate e fallito:" >&2; tail -c 1500 /tmp/xtk_phpcfg.out >&2; exit 5; }
echo "config php per $name/$vhost applicata (ini:$([ -n "$ini_out" ] && echo si || echo no) fpm:$([ -n "$fpm_raw" ] && echo si || echo no))"
