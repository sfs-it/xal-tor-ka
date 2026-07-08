FROM alpine:3.20
RUN apk add --no-cache openssh-server openssl && rm -f /etc/ssh/ssh_host_* \
    && mkdir -p /etc/ssh/keys /run/sshd /var/empty
COPY sshd_config /etc/ssh/sshd_config
EXPOSE 22
# Persist host keys under /etc/ssh/keys (mounted volume); generate on first run.
ENTRYPOINT ["/bin/sh","-c","[ -f /etc/ssh/keys/ssh_host_ed25519_key ] || ssh-keygen -t ed25519 -f /etc/ssh/keys/ssh_host_ed25519_key -N ''; [ -f /etc/ssh/keys/ssh_host_rsa_key ] || ssh-keygen -t rsa -b 3072 -f /etc/ssh/keys/ssh_host_rsa_key -N ''; exec /usr/sbin/sshd -D -e"]
