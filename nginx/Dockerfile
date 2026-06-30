FROM nginx:stable-alpine

# Il watcher ricarica NGINX quando il proxy manager rigenera
# /etc/nginx/conf.d/backends.conf. Usa polling (md5sum/cat/sleep, tutti built-in
# di busybox) → nessuna dipendenza aggiuntiva.

COPY entrypoint.sh /xtk-entrypoint.sh
RUN chmod +x /xtk-entrypoint.sh

ENTRYPOINT ["/xtk-entrypoint.sh"]
