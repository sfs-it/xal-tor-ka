#!/bin/bash
# Read-only inventory of sites as a JSON array. Each site carries LEGACY FLAT fields
# (name/uid/running/template/php_version/db/auto_update — mirroring its httpdocs/primary
# vhost, so existing consumers keep working) PLUS a nested "vhosts" array (the multi-vhost
# model). A legacy single-docker site (top-level compose, not yet migrated) is reported as
# one synthetic "httpdocs" vhost. Site names come from a strict pattern → safe to embed.
set -euo pipefail
source "$(dirname "$0")/_vhost_lib.sh"

# add_vhost <vhost> <vdir> — appends a vhost JSON object to $vhosts and updates the
# per-site accumulators ($total, $top_*). Uses dynamic scope: name/vhosts/total/vfirst/
# top_* live in the calling loop. <vdir> holds .xtk-stack, db.env and docker-compose.yml
# (the vhost config dir for the new layout, or the site root for a legacy site).
add_vhost() {
  local vhost="$1" vdir="$2" tmpl="" pv="" au=false db="" dom="" pe="" proj al running
  local c_umf="" c_pms="" c_mem="" c_mfu="" c_met="" c_ini="" c_fpm=""
  if [ -f "$vdir/.xtk-stack" ]; then
    tmpl="$(sed -n 's/^template=//p' "$vdir/.xtk-stack")"
    pv="$(sed -n 's/^php_version=//p' "$vdir/.xtk-stack")"
    dom="$(sed -n 's/^domain=//p' "$vdir/.xtk-stack")"
    pe="$(sed -n 's/^php_extensions=//p' "$vdir/.xtk-stack")"   # moduli à-la-carte (csv)
    [ "$(sed -n 's/^auto_update=//p' "$vdir/.xtk-stack")" = true ] && au=true
    c_umf="$(sed -n 's/^php_upload_max_filesize=//p' "$vdir/.xtk-stack")"
    c_pms="$(sed -n 's/^php_post_max_size=//p' "$vdir/.xtk-stack")"
    c_mem="$(sed -n 's/^php_memory_limit=//p' "$vdir/.xtk-stack")"
    c_mfu="$(sed -n 's/^php_max_file_uploads=//p' "$vdir/.xtk-stack")"
    c_met="$(sed -n 's/^php_max_execution_time=//p' "$vdir/.xtk-stack")"
    c_ini="$(sed -n 's/^php_ini_raw_b64=//p' "$vdir/.xtk-stack")"   # base64 (frame libero php.ini)
    c_fpm="$(sed -n 's/^php_fpm_raw_b64=//p' "$vdir/.xtk-stack")"   # base64 (frame libero php-fpm)
  fi
  [ "$tmpl" = php-fpm ] || pv=""   # php_version is meaningful only for php-fpm (fixes stale "static · 8.3")
  if [ -f "$vdir/db.env" ]; then
    case "$(sed -n 's/^DB_HOST=//p' "$vdir/db.env")" in *mysql*) db=mysql;; *pg*) db=pg;; esac
  fi
  proj="$(vhost_project "$name" "$vhost")"; al="$(vhost_alias "$name" "$vhost")"
  running="$( { docker compose --project-directory "$XTK_SITES/$name" -f "$vdir/docker-compose.yml" -p "$proj" ps -q 2>/dev/null || true; } | wc -l | tr -d ' ')"
  [ "$vfirst" -eq 1 ] || vhosts+=','; vfirst=0
  vhosts+="$(printf '{"vhost":"%s","domain":"%s","template":"%s","php_version":"%s","db":"%s","auto_update":%s,"running":%s,"upstream":"http://%s:8080","php_extensions":"%s","php_cfg":{"upload_max_filesize":"%s","post_max_size":"%s","memory_limit":"%s","max_file_uploads":"%s","max_execution_time":"%s","ini_raw_b64":"%s","fpm_raw_b64":"%s"}}' \
    "$vhost" "$dom" "$tmpl" "$pv" "$db" "$au" "$running" "$al" "$pe" \
    "$c_umf" "$c_pms" "$c_mem" "$c_mfu" "$c_met" "$c_ini" "$c_fpm")"
  total=$((total + running))
  if [ "$vhost" = httpdocs ]; then top_tmpl="$tmpl"; top_pv="$pv"; top_db="$db"; top_au="$au"; top_dom="$dom"; fi
}

first=1; printf '['
if [ -d "$XTK_SITES" ]; then
  for d in "$XTK_SITES"/*/; do
    name="$(basename "$d")"
    [[ "$name" =~ ^[a-z][a-z0-9-]{1,30}$ ]] || continue
    uid="$(id -u "site-$name" 2>/dev/null || stat -c '%u' "$d")"
    vhosts=""; total=0; vfirst=1; top_tmpl=""; top_pv=""; top_db=""; top_au=false; top_dom=""; legacy=true
    if [ -d "$d/.vhosts" ]; then
      legacy=false
      for vd in "$d"/.vhosts/*/; do
        [ -f "$vd/docker-compose.yml" ] || continue
        add_vhost "$(basename "$vd")" "${vd%/}"
      done
    elif [ -f "$d/docker-compose.yml" ]; then
      add_vhost httpdocs "${d%/}"   # legacy single-docker site: files live at the site root
    else
      continue
    fi
    [ $first -eq 1 ] || printf ','; first=0
    printf '{"name":"%s","uid":%s,"domain":"%s","running":%s,"template":"%s","php_version":"%s","db":"%s","auto_update":%s,"legacy":%s,"vhosts":[%s]}' \
      "$name" "$uid" "$top_dom" "$total" "$top_tmpl" "$top_pv" "$top_db" "$top_au" "$legacy" "$vhosts"
  done
fi
printf ']\n'
