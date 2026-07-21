# Gate nginx WITH ModSecurity v3 + OWASP CRS (per-vhost WAF, opt-in).
# Base image ships the compiled ModSecurity nginx module + the CRS; we drop in a
# static nginx.conf (loads the module, WAF off globally) and the two shared rule
# files (block / detect), and keep the polling reload watcher as entrypoint.
FROM owasp/modsecurity-crs:nginx-alpine

# The gate binds :80/:443 and reloads nginx — run as root (like the original image).
USER root

# Static gate config (bypasses the base image's env-templated nginx.conf).
COPY nginx.conf /etc/nginx/nginx.conf

# Shared per-vhost WAF rule files (selected by mode in the generated backends.conf).
COPY modsec/xtk-block.conf  /etc/modsecurity.d/xtk-block.conf
COPY modsec/xtk-detect.conf /etc/modsecurity.d/xtk-detect.conf

# Reload watcher (polls conf.d + certs, reloads on change if `nginx -t` passes).
COPY entrypoint.sh /xtk-entrypoint.sh
RUN chmod +x /xtk-entrypoint.sh

# Override the base image's healthcheck (it curls https://localhost:$SSL_PORT/healthz
# with SSL_PORT=8443 — a port this static config does NOT listen on → always exit 7).
# Ours hits the dedicated internal liveness endpoint on :81 (nginx itself, no upstream).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fs http://localhost:81/healthz || exit 1

ENTRYPOINT ["/xtk-entrypoint.sh"]
