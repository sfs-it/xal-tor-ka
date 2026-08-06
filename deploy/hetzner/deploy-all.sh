#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
#
# Orchestrator: runs the Hetzner node deploy stages in slot order (0..9).
#   Generic stages (this repo): 0 preflight · 1 provision · 2 base OS ·
#                               4 Xal-Tor-Ka · 5 hosting
#   Optional ADDONS (org-specific, OUTSIDE this repo): declared in the machine env
#   as  XTK_ADDONS="3:/abs/path/foo.sh 6:/abs/path/bar.sh"  — external scripts that
#   slot into the sequence at the given digit and source lib.sh via $XTK_HETZNER_LIB.
#   (Keeps private provisioning — users, private repos — out of this public repo.)
#
# DRY-RUN by default; pass --apply to execute. --from/--to/--only filter by slot.
#   ./deploy-all.sh                 # dry-run all slots
#   ./deploy-all.sh --apply         # execute all
#   ./deploy-all.sh --only 4 --apply
#   XTK_HETZNER_ENV=/path/secret.env ./deploy-all.sh --apply
set -euo pipefail
HZ_DIR="$(cd "$(dirname "$0")" && pwd)"

FROM=0; TO=9; ONLY=""; APPLY=0
while [ $# -gt 0 ]; do
	case "$1" in
		--apply) APPLY=1 ;;
		--from)  FROM="$2"; shift ;;
		--to)    TO="$2"; shift ;;
		--only)  ONLY="$2"; shift ;;
		-h|--help) sed -n '2,16p' "$0"; exit 0 ;;
		*) echo "unknown arg: $1" >&2; exit 2 ;;
	esac; shift
done
export DRY_RUN=$(( APPLY == 1 ? 0 : 1 ))
export XTK_HETZNER_LIB="${HZ_DIR}/lib.sh"
[ -n "$ONLY" ] && { FROM="$ONLY"; TO="$ONLY"; }

# fixed generic stages (this repo)
declare -A SLOT
SLOT[0]="${HZ_DIR}/00-preflight.sh"
SLOT[1]="${HZ_DIR}/10-provision.sh"
SLOT[2]="${HZ_DIR}/20-baseos.sh"
SLOT[4]="${HZ_DIR}/40-xaltorka.sh"
SLOT[5]="${HZ_DIR}/50-hosting.sh"

# optional addons from the machine env — XTK_ADDONS="N:/abs/path ..." (N = slot 0-9)
ENVF="${XTK_HETZNER_ENV:-${HZ_DIR}/hetzner.env}"
if [ -f "$ENVF" ]; then
	ADDONS="$(tr -d '\r' < "$ENVF" | sed -n 's/^XTK_ADDONS=//p' | sed 's/^"//; s/"$//')"
	for a in $ADDONS; do
		n="${a%%:*}"; p="${a#*:}"
		case "$n" in [0-9]) SLOT[$n]="$p" ;; *) echo "bad XTK_ADDONS entry (need N:/path): $a" >&2; exit 2 ;; esac
	done
fi

banner() { printf '\n\033[1;44m %s \033[0m\n' "$*"; }
if [ "$DRY_RUN" = 1 ]; then banner "DRY-RUN — no changes. Add --apply to execute."
else banner "APPLY MODE — slots ${FROM}..${TO} WILL run (rebuild wipes the disk)."; fi

for i in 0 1 2 3 4 5 6 7 8 9; do
	[ "$i" -ge "$FROM" ] && [ "$i" -le "$TO" ] || continue
	[ -n "${SLOT[$i]:-}" ] || continue
	[ -f "${SLOT[$i]}" ] || { echo "slot $i: ${SLOT[$i]} not found" >&2; exit 1; }
	banner "STAGE ${i}: $(basename "${SLOT[$i]}")"
	DRY_RUN="$DRY_RUN" XTK_HETZNER_LIB="$XTK_HETZNER_LIB" bash "${SLOT[$i]}" \
		|| { echo "STAGE ${i} ($(basename "${SLOT[$i]}")) FAILED — stopping." >&2; exit 1; }
done

banner "done (slots ${FROM}..${TO}, $( [ "$DRY_RUN" = 1 ] && echo dry-run || echo applied ))"
