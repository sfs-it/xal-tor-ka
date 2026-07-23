#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# Deploy the current commit to a remote host running the Docker stack.
#
# It encodes the deploy sequence that was previously done by hand, including the
# two steps that are easy to forget and expensive to miss:
#
#   * the ownership of nginx/conf.d, which the tar resets to root and which the
#     core (uid 1000) needs in order to regenerate backends.conf. Getting this
#     wrong degrades the gateway SILENTLY: the admin UI keeps working, but no
#     publish and no certificate ever reaches NGINX.
#   * BUILD and UP as separate steps, with SSH keep-alives, because a build can
#     run for minutes and a dropped connection mid-build leaves a half-created
#     container that makes the retry fail.
#
# A rollback tag is always taken before anything is replaced, and the rollback
# command is printed if any step fails.
#
# No host, key or secret is hardcoded: this repository is public. Pass them via
# the environment (see below). On the maintainer's machines the whole run is
# wrapped for auditing:
#
#   run+log --project xtk-agent -- ./scripts/deploy.sh
#
# Environment:
#   XTK_HOST        user@host of the target machine                  (required)
#   XTK_SSH_KEY     path to the SSH private key                      (required)
#   XTK_REMOTE_DIR  stack directory on the target      (default: /opt/xaltorka)
#   XTK_SERVICES    compose services to rebuild             (default: xaltorka)
#   XTK_CORE_NAME   core container name    (default: xaltorka-xaltorka-1)
#   XTK_URL         URL to check after the deploy         (optional, e.g. the gate)
#
set -euo pipefail

HOST="${XTK_HOST:?set XTK_HOST (user@host)}"
KEY="${XTK_SSH_KEY:?set XTK_SSH_KEY (path to the ssh key)}"
REMOTE_DIR="${XTK_REMOTE_DIR:-/opt/xaltorka}"
SERVICES="${XTK_SERVICES:-xaltorka}"
CORE_NAME="${XTK_CORE_NAME:-xaltorka-xaltorka-1}"
URL="${XTK_URL:-}"

# Directories the core must be able to write after the archive is extracted.
# The tar recreates them owned by root; the core runs as uid 1000.
CORE_OWNED_DIRS="nginx/conf.d"
CORE_UID=1000

SSH_OPTS=(-i "$KEY" -o StrictHostKeyChecking=no -o ConnectTimeout=20
          -o ServerAliveInterval=15 -o ServerAliveCountMax=40)
say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
remote() { ssh "${SSH_OPTS[@]}" "$HOST" "$@"; }

VERSION=$(sed -n 's/.*Version = "\(.*\)".*/\1/p' version/version.go)
BRANCH=$(git rev-parse --abbrev-ref HEAD)
COMMIT=$(git rev-parse --short HEAD)
STAMP=$(date +%Y%m%d-%H%M%S)
ROLLBACK_TAG="xaltorka:rollback-${STAMP}"
ARCHIVE="/tmp/xtk-${VERSION}-${COMMIT}.tar.gz"

say "Deploying ${VERSION} (${BRANCH} @ ${COMMIT}) to ${HOST}:${REMOTE_DIR}"

if ! git diff --quiet || ! git diff --cached --quiet; then
	echo "working tree is dirty: commit or stash first (the archive is built from HEAD)" >&2
	exit 1
fi

on_error() {
	cat >&2 <<EOF

DEPLOY FAILED. The previous image is tagged ${ROLLBACK_TAG}; to roll back:
  ssh -i <key> ${HOST} 'cd ${REMOTE_DIR} && docker tag ${ROLLBACK_TAG} xaltorka:latest && docker compose up -d ${SERVICES}'
EOF
}
trap on_error ERR

say "1/7 rollback tag (${ROLLBACK_TAG})"
remote "docker tag xaltorka:latest ${ROLLBACK_TAG}"

say "2/7 archive HEAD and transfer"
git archive --format=tar HEAD | gzip >"$ARCHIVE"
scp -i "$KEY" -o StrictHostKeyChecking=no -o ConnectTimeout=20 "$ARCHIVE" "${HOST}:${ARCHIVE}"

# --anchored keeps the excludes at the root: the core compose file and config.json
# belong to the machine, while ext/*/docker-compose.yml must still be updated.
say "3/7 extract (keeping the machine's docker-compose.yml and config.json)"
remote "cd ${REMOTE_DIR} && tar -xzf ${ARCHIVE} --no-same-owner --anchored \
	--exclude=docker-compose.yml --exclude=config.json && rm -f ${ARCHIVE}"

say "4/7 restore the directories the core must write (${CORE_OWNED_DIRS})"
remote "cd ${REMOTE_DIR} && chown ${CORE_UID}:${CORE_UID} ${CORE_OWNED_DIRS} && ls -ldn ${CORE_OWNED_DIRS}"
# Assert it, instead of trusting it: this is the silent failure the whole script exists for.
owner=$(remote "stat -c %u ${REMOTE_DIR}/nginx/conf.d")
[ "$owner" = "$CORE_UID" ] || { echo "nginx/conf.d is owned by uid ${owner}, expected ${CORE_UID}" >&2; exit 1; }

say "5/7 build (separate from up, so a slow build cannot leave a half-created container)"
remote "cd ${REMOTE_DIR} && docker compose build ${SERVICES}"

say "6/7 up (only the services that changed)"
remote "cd ${REMOTE_DIR} && docker compose up -d ${SERVICES}"

say "7/7 verify"
sleep 5
boot=$(remote "docker logs --since 90s ${CORE_NAME} 2>&1" || true)
printf '%s\n' "$boot" | grep -E 'level=(ERROR|WARN)|CRITICAL' || echo "boot log: no ERROR/WARN/CRITICAL"
if printf '%s\n' "$boot" | grep -q 'CRITICAL'; then
	echo "the gateway reported a CRITICAL condition at boot (see the line above)" >&2
	exit 1
fi
printf '%s\n' "$boot" | grep -E 'msg=xal-tor-ka|config loaded' || true
remote "cd ${REMOTE_DIR} && docker compose ps --format '{{.Name}} | {{.Status}}'"
if [ -n "$URL" ]; then
	code=$(curl -sL -o /dev/null -m 15 -w '%{http_code}' "$URL" || true)
	echo "${URL} -> ${code}"
fi

trap - ERR
rm -f "$ARCHIVE"
say "done — ${VERSION} is live (rollback image: ${ROLLBACK_TAG})"
