#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# STAGE 4 — install Xal-Tor-Ka (the gate). FIRST install, so it follows INSTALL.md
# (clone -> .env -> make up -> cert -> admin), NOT scripts/deploy.sh (that one is
# for UPDATES: its rollback-tag step needs an image that a virgin box lacks).
#
# TLS: if XTK_ACME_EMAIL and XTK_DOMAIN are set -> TLS_MODE=acme and a real
# Let's Encrypt cert is issued via HTTP-01 (no chicken-egg: the proxy generator
# emits `listen 443` only once a cert exists, so the box serves :80 + the ACME
# challenge until issuance succeeds). XTK_ACME_STAGING=1 uses the LE staging CA
# (untrusted, no rate limits) to prove the flow; =0 issues the real prod cert.
# Without an ACME email it falls back to TLS_MODE=selfsigned.
set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

XTK_REPO_URL="${XTK_REPO_URL:-https://github.com/sfs-it/xal-tor-ka.git}"
XTK_REF="${XTK_REF:-main}"                     # main = stable (beta0.14); set beta0.15 for dev
XTK_REMOTE_DIR="${XTK_REMOTE_DIR:-/opt/xaltorka}"
XTK_ADMIN_CIDR="${XTK_ADMIN_CIDR:-0.0.0.0/0}"  # TEST default; TIGHTEN for production
XTK_ADMIN_EMAIL="${XTK_ADMIN_EMAIL:-admin@localhost}"
LE_STAGING_URL="https://acme-staging-v02.api.letsencrypt.org/directory"

say "STAGE 4 — install Xal-Tor-Ka (${XTK_REF}) in ${XTK_REMOTE_DIR}"; mode_banner
load_env
require_vars HCLOUD_SERVER_IP
[ "$XTK_ADMIN_CIDR" = "0.0.0.0/0" ] && warn "ADMIN_CIDR is 0.0.0.0/0 (open) — fine for a test node, TIGHTEN before production"

# TLS mode: acme when we have both an ACME email and a real domain, else selfsigned.
if [ -n "${XTK_ACME_EMAIL:-}" ] && [ -n "${XTK_DOMAIN:-}" ]; then TLS_MODE=acme; else TLS_MODE=selfsigned; fi
GATE_URL="${XTK_DOMAIN:+https://$XTK_DOMAIN}"; GATE_URL="${GATE_URL:-https://${HCLOUD_SERVER_IP}}"
say "TLS_MODE=${TLS_MODE}  GATE_URL=${GATE_URL}  ACME_EMAIL=${XTK_ACME_EMAIL:-<none>}  STAGING=${XTK_ACME_STAGING:-1}"

say "1/7 clone/update repo (${XTK_REPO_URL} @ ${XTK_REF})"
# Robust branch switch: config.json is edited in place (3-ter), so a plain checkout
# across branches would abort on local changes. Force-checkout + hard-reset to the
# target ref (3-ter re-applies the ACME email; gitignored secrets are untouched).
ssh_run "mkdir -p ${XTK_REMOTE_DIR} && if [ -d ${XTK_REMOTE_DIR}/.git ]; then cd ${XTK_REMOTE_DIR} && git config --global --add safe.directory ${XTK_REMOTE_DIR} 2>/dev/null || true; git fetch --all -q && git checkout -f ${XTK_REF} && git reset --hard \"origin/${XTK_REF}\" -q; else git clone --branch ${XTK_REF} ${XTK_REPO_URL} ${XTK_REMOTE_DIR}; fi"

if [ "$TLS_MODE" = "acme" ]; then
	say "1-bis/7 patch default nginx server to serve the ACME HTTP-01 challenge for the gate's OWN domain"
	# The gate FQDN is served by the default_server (server_name _), which proxies
	# everything to the core -> /.well-known/acme-challenge/ 404s. generate.go adds the
	# challenge only to per-backend blocks, so the gate cannot get its own cert via
	# HTTP-01. Inject a static challenge location (webroot = the shared cert dir) into
	# the default server, before `location /`. Idempotent. (TODO: fix upstream in XalTorKa.)
	ssh_run "cd ${XTK_REMOTE_DIR} && grep -q acme-challenge nginx/conf.d/xaltorka.conf || sed -i 's|^\\([[:space:]]*\\)location / {|\\1location /.well-known/acme-challenge/ { default_type text/plain; root /etc/nginx/certs; }\\n\\1location / {|' nginx/conf.d/xaltorka.conf; grep -nE 'acme-challenge|location / \\{' nginx/conf.d/xaltorka.conf"
fi

say "2/7 write .env (GATE_URL=${GATE_URL}, TLS=${TLS_MODE}, ADMIN_CIDR=${XTK_ADMIN_CIDR})"
ssh_run "cat > ${XTK_REMOTE_DIR}/.env <<'ENV'
HTTP_PORT=80
GATE_URL=${GATE_URL}
TLS_MODE=${TLS_MODE}
ADMIN_CIDR=${XTK_ADMIN_CIDR}
EDGE_CIDR=172.16.0.0/12
PUID=1000
PGID=1000
ENV"

say "3/7 bootstrap secrets.json / users.json / services.json (gitignored -> absent in clone)"
# The compose mounts the whole dir at /etc/xaltorka. Any gitignored config file
# that is MISSING gets materialised by Docker as a DIRECTORY -> the core reads a
# directory and dies (services.json was the gap that crashlooped the core -> 502).
ssh_run "cd ${XTK_REMOTE_DIR} && \
  for pair in secrets.json:secrets.example.json services.json:services.example.json; do \
    dst=\${pair%%:*}; src=\${pair##*:}; \
    [ -d \"\$dst\" ] && rm -rf \"\$dst\"; \
    [ -f \"\$dst\" ] || cp \"\$src\" \"\$dst\"; \
  done; \
  [ -d users.json ] && rm -rf users.json; [ -f users.json ] || printf '{ \"users\": [] }\n' > users.json; \
  chmod 600 secrets.json users.json services.json; ls -la secrets.json users.json services.json config.json"

if [ "$TLS_MODE" = "acme" ]; then
	say "3-ter/7 set ACME email + dummy pdns_api_url in config.json (HTTP-01 doesn't use pdns; validate.go still requires it)"
	ssh_run "cd ${XTK_REMOTE_DIR} && sed -i 's|\"pdns_api_url\": \"\"|\"pdns_api_url\": \"http01-unused\"|; s|\"email\": \"\"|\"email\": \"${XTK_ACME_EMAIL}\"|' config.json && grep -o '\"acme\":[^}]*}' config.json"
fi

# The core runs as uid 1000 and must READ secrets/users/config/services.json and
# WRITE nginx/conf.d, data, certs, logs, backups. A fresh clone leaves them
# root-owned -> 'permission denied' fatal / silent config failure.
say "3-bis/7 give uid 1000 ownership of the WHOLE config dir (silent-failure guard)"
# The core runs as uid 1000 and writes secrets/users/services.json ATOMICALLY
# (writes <file>.tmp in the SAME dir, then renames). If /opt/xaltorka itself is
# root-owned, uid 1000 cannot create the .tmp -> 'permission denied' -> SaveUsers/
# SaveSetup fail -> the setup wizard 403s and admin seeding fails. Chowning the
# individual files is NOT enough: the DIRECTORY must be writable. safe.directory
# lets root's git pull operate on the now-1000-owned tree on re-runs.
ssh_run "cd ${XTK_REMOTE_DIR} && mkdir -p nginx/conf.d data certs logs backups && chown -R 1000:1000 ${XTK_REMOTE_DIR} && git config --global --add safe.directory ${XTK_REMOTE_DIR} 2>/dev/null || true; ls -ldn ${XTK_REMOTE_DIR}"

say "4/7 build + start (force-recreate so a stale crashlooping core is replaced)"
ssh_run "cd ${XTK_REMOTE_DIR} && docker compose up -d --build --force-recreate"

say "5/7 healthcheck on :80 (POLL until it RESPONDS; dump core logs on failure)"
ssh_run "cd ${XTK_REMOTE_DIR}; for i in \$(seq 1 30); do curl -fsS http://localhost:80/healthz >/dev/null 2>&1 && { echo 'healthz OK'; exit 0; }; sleep 3; done; echo 'healthz did not answer after 90s — core logs:'; docker compose logs --tail 50 xaltorka 2>&1 | tail -50; docker compose ps; exit 1"

if [ "$TLS_MODE" = "acme" ]; then
	acme_env=""
	if [ "${XTK_ACME_STAGING:-1}" = "1" ]; then acme_env="-e ACME_DIRECTORY_URL=${LE_STAGING_URL}"; warn "ACME STAGING (cert will be UNTRUSTED — proving the flow; set XTK_ACME_STAGING=0 for a real cert)"; fi
	target_ca=prod; [ "${XTK_ACME_STAGING:-1}" = "1" ] && target_ca=staging
	# staging and prod are DIFFERENT ACME accounts: reusing a staging account key
	# against prod fails. On a CA switch, clear the account key + old cert so a fresh
	# account/cert is minted. A marker file records which CA the account belongs to.
	say "6-pre/7 match ACME account to target CA (${target_ca})"
	ssh_run "cd ${XTK_REMOTE_DIR} && cur=\$(cat certs/.acme_ca 2>/dev/null || echo none); if [ \"\$cur\" != \"${target_ca}\" ]; then echo \"ACME CA switch \$cur -> ${target_ca}: clearing account key + old cert\"; rm -f certs/acme_account.key certs/${XTK_DOMAIN}.crt certs/${XTK_DOMAIN}.key; printf '%s' '${target_ca}' > certs/.acme_ca; chown 1000:1000 certs/.acme_ca; else echo \"account already for ${target_ca}\"; fi"

	# Skip issuance if a valid cert (>30d) already exists — idempotent re-deploys
	# must NOT churn Let's Encrypt certs (5 duplicates/domain/week rate limit). The
	# 6-pre CA-switch above deletes the cert only when switching staging<->prod, so a
	# surviving cert is the right CA's.
	skip_issue=0
	if ! dry; then
		mapfile -t _o < <(ssh_opts)
		if ssh "${_o[@]}" "root@${HCLOUD_SERVER_IP}" "openssl x509 -checkend 2592000 -noout -in ${XTK_REMOTE_DIR}/certs/${XTK_DOMAIN}.crt" 2>/dev/null; then
			ok "6/7 valid cert already present (>30d) — skipping ACME issuance (avoids LE rate limits)"; skip_issue=1
		fi
	fi
	if [ "$skip_issue" = 0 ]; then
		say "6/7 issue ACME cert for ${XTK_DOMAIN} via HTTP-01 (staging=${XTK_ACME_STAGING:-1})"
		# One-off container issues (writes cert + challenge tokens to the shared certs/
		# webroot the running nginx serves on :80). --config MUST precede the subcommand:
		# Go's flag parser stops at the first positional arg, so `cert issue <host>
		# --config …` would ignore --config and read ./config.json (permission denied).
		ssh_run "cd ${XTK_REMOTE_DIR} && docker compose run --rm ${acme_env} xaltorka cert --config /etc/xaltorka issue ${XTK_DOMAIN}"
	fi
	# The gate FQDN is served by the default_server, which generate.go never gives a
	# 443 listener (that logic is per-backend). Now that the cert EXISTS (post-issuance,
	# so no chicken-egg), add `listen 443 ssl` + the gate cert to the default server.
	# Idempotent. (TODO: fix upstream — the gate should serve its own HTTPS natively.)
	say "6-bis/7 add 443 listener with the gate cert to the default nginx server"
	ssh_run "cd ${XTK_REMOTE_DIR} && grep -q 'listen 443' nginx/conf.d/xaltorka.conf || sed -i \"s|listen 80 default_server;|listen 80 default_server;\\n    listen 443 ssl default_server;\\n    ssl_certificate /etc/nginx/certs/${XTK_DOMAIN}.crt;\\n    ssl_certificate_key /etc/nginx/certs/${XTK_DOMAIN}.key;|\" nginx/conf.d/xaltorka.conf; grep -nE 'listen (80|443)|ssl_certificate ' nginx/conf.d/xaltorka.conf"
	ssh_run "cd ${XTK_REMOTE_DIR} && docker compose exec -T nginx nginx -t && docker compose restart nginx && sleep 5"
	say "6-ter/7 verify :443 serves"
	ssh_run "cd ${XTK_REMOTE_DIR}; for i in \$(seq 1 10); do curl -fsSk https://localhost:443/healthz >/dev/null 2>&1 && { echo '443 OK (TLS serving)'; exit 0; }; sleep 3; done; echo '443 not serving — acme/cert logs:'; docker compose logs --tail 30 xaltorka 2>&1 | grep -iE 'acme|cert|tls|443|error|fatal' | tail -20; exit 1"
fi

say "7/7 seed the initial administrator (install-ready — no manual setup-token dance)"
# Create the admin DIRECTLY via the CLI (writes users.json), then RESTART the core
# so it reloads users.json into memory (a one-off CLI cannot update the running
# process — that was the whole "setup wizard" pain: the browser token flow works
# server-side, but is fragile to token supersession; seeding removes the footgun).
# Password: from XTK_ADMIN_PASSWORD if set, else generated and saved to a 600 file
# on the box. $PW is expanded REMOTE-side, so it never lands in the deploy log.
# Idempotent: if the admin already exists, DO NOT reset its password on a re-deploy
# (the operator may have changed it). Seed only on first install.
ssh_run "cd ${XTK_REMOTE_DIR}; if grep -q '\"email\"[[:space:]]*:[[:space:]]*\"${XTK_ADMIN_EMAIL}\"' users.json 2>/dev/null; then echo 'admin ${XTK_ADMIN_EMAIL} already exists — keeping current credentials (no reset)'; else PW='${XTK_ADMIN_PASSWORD:-}'; [ -n \"\$PW\" ] || PW=\$(openssl rand -base64 18 2>/dev/null | tr -dc 'A-Za-z0-9' | cut -c1-20); docker compose run --rm xaltorka user --email '${XTK_ADMIN_EMAIL}' --password \"\$PW\" --admin --config /etc/xaltorka || { echo 'ADMIN SEED FAILED'; exit 1; }; umask 077; printf 'admin=%s\npassword=%s\n' '${XTK_ADMIN_EMAIL}' \"\$PW\" > .admin-initial-credentials; chmod 600 .admin-initial-credentials; docker compose restart xaltorka >/dev/null 2>&1; sleep 5; echo 'admin seeded + core reloaded'; fi; docker compose logs --tail 10 xaltorka | grep -oE 'users=[0-9]+' | tail -1"

say "7-bis/7 show where the initial admin credentials are (read once, change after login)"
ssh_run "cd ${XTK_REMOTE_DIR} && echo 'initial admin creds: /opt/xaltorka/.admin-initial-credentials (chmod 600)' && ls -ln .admin-initial-credentials"

say "Xal-Tor-Ka stage complete — gate at ${GATE_URL} · login: ${XTK_ADMIN_EMAIL} (pw in .admin-initial-credentials)"
