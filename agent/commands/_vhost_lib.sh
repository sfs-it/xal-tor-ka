# shellcheck shell=bash
# _vhost_lib.sh — shared helpers for the per-vhost hosting model. SOURCED by
# site_create.sh and the vhost_*.sh commands; it is NOT a command itself (no .json
# manifest → the agent never registers or execs it directly). root:root, like every
# vetted script. It contains NO parameter-driven shell: callers pass already-validated
# site/vhost names, and every function re-validates (defense in depth).
#
# Model: a *site* is the OS user `site-<name>` + the root-owned dir /opt/sites/<name>
# (the SFTP chroot). Inside it live N *vhosts*, each its own docker (web[+php]):
#   /opt/sites/<name>/<vhost>/            docroot (site-user-owned)   e.g. httpdocs, app
#   /opt/sites/<name>/logs/<vhost>/       nginx logs (site-user-owned)
#   /opt/sites/<name>/.vhosts/<vhost>/    compose+nginx+db.env+.xtk-stack (root-owned, ro)
# Composes run with `--project-directory /opt/sites/<name>` so their relative paths
# resolve from the SHARED site root.
: "${XTK_SITES:=/opt/sites}"
: "${XTK_TEMPLATES:=/usr/local/lib/xtk-agent/templates}"

vhost_valid_name() { [[ "$1" =~ ^[a-z][a-z0-9-]{1,30}$ ]]; }

# The first vhost, "httpdocs", keeps the LEGACY compose project name and network alias
# (`<name>` / `<name>.site`) so migrating an existing single-docker site never changes
# its already-published gateway backend. Extra vhosts get `<name>-<vhost>`.
vhost_project() { if [ "$2" = httpdocs ]; then echo "$1"; else echo "$1-$2"; fi; }
vhost_alias()   { if [ "$2" = httpdocs ]; then echo "$1.site"; else echo "$1-$2.site"; fi; }

# render_vhost <site> <vhost> <template> <php_version> <uid> <gid>
# Creates the vhost's config dir, docroot and logs dir under an EXISTING (root-owned)
# site dir, renders the template, and sets ownership. Echoes the gateway alias on
# success (its only stdout). Returns 2 (bad template) / 3 (vhost exists) on error.
render_vhost() {
  local name="$1" vhost="$2" tmpl="$3" pv="$4" uid="$5" gid="$6"
  local dir="$XTK_SITES/$name" src="$XTK_TEMPLATES/$tmpl"
  local vdir="$dir/.vhosts/$vhost" docroot="$dir/$vhost" logs="$dir/logs/$vhost"
  local al; al="$(vhost_alias "$name" "$vhost")"
  [ -d "$src" ] || { echo "unknown template: $tmpl" >&2; return 2; }
  [ -e "$vdir" ] && { echo "vhost already exists: $name/$vhost" >&2; return 3; }

  mkdir -p "$vdir" "$docroot" "$logs"
  cp "$src/docker-compose.yml" "$vdir/docker-compose.yml"
  cp "$src/nginx.conf"         "$vdir/nginx.conf"
  if [ -f "$src/db.env" ]; then cp "$src/db.env" "$vdir/db.env"; else : > "$vdir/db.env"; fi
  # seed the docroot from the template (index.php / index.html) only if empty
  if [ -d "$src/httpdocs" ] && [ -z "$(ls -A "$docroot" 2>/dev/null)" ]; then
    cp -a "$src/httpdocs/." "$docroot/"
  fi

  local subst="s|__NAME__|$name|g; s|__VHOST__|$vhost|g; s|__ALIAS__|$al|g; s|__UID__|$uid|g; s|__GID__|$gid|g; s|__PHP_VERSION__|$pv|g"
  sed -i "$subst" "$vdir/docker-compose.yml" "$vdir/nginx.conf"
  local f; for f in "$docroot"/index.php "$docroot"/index.html; do
    [ -f "$f" ] && sed -i "$subst" "$f" || true
  done

  # record the stack so the UI can show it and auto-update can tell pristine from edited.
  # php_version is meaningful only for php-fpm (empty otherwise).
  if [ "$tmpl" = php-fpm ]; then
    printf 'template=%s\nphp_version=%s\nauto_update=false\n' "$tmpl" "$pv" > "$vdir/.xtk-stack"
  else
    printf 'template=%s\nphp_version=\nauto_update=false\n' "$tmpl" > "$vdir/.xtk-stack"
  fi

  # ownership: config stays root-owned (mounted read-only); only the docroot and the
  # logs dir are writable by the site user. The site dir itself must stay root:root
  # 0755 for the SFTP chroot — we never touch it here.
  chown -R root:root "$vdir"; chmod 0755 "$vdir"
  chown -R "$uid:$gid" "$docroot" "$logs"
  echo "$al"
}

# for_each_vhost <site> <fn> — call `fn <site> <vhost>` for every vhost of a site
# (new layout). Prints nothing itself. Returns 0 even if the site has no .vhosts dir.
for_each_vhost() {
  local name="$1" fn="$2" f v
  for f in "$XTK_SITES/$name"/.vhosts/*/docker-compose.yml; do
    [ -f "$f" ] || continue
    v="$(basename "$(dirname "$f")")"
    "$fn" "$name" "$v"
  done
}
