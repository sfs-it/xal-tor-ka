#!/bin/bash
# Dry-run a selective upgrade of the named packages: report EXACTLY what apt would do
# (which packages upgrade/install, which get REMOVED) WITHOUT changing anything. This is
# the preventive dependency check — apt/dpkg resolve and order; we just surface the plan.
# Param XTK_P_PACKAGES = space-separated package names (re-validated here).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

raw="${XTK_P_PACKAGES:-}"
[ -n "$raw" ] || { echo "no packages given" >&2; exit 2; }
pkgs=()
for t in $raw; do
  [[ "$t" =~ ^[a-z0-9][a-z0-9+.-]+$ ]] || { echo "invalid package name: $t" >&2; exit 2; }
  pkgs+=("$t")
done

# -s = simulate. Machine-readable lines: "Inst NAME ..." (upgrade/install), "Remv NAME ..." (remove).
sim="$(apt-get install --only-upgrade -s "${pkgs[@]}" 2>&1)" || { echo "apt simulate failed" >&2; exit 1; }

insts="$(printf '%s\n' "$sim" | awk '/^Inst /{print $2}')"
remvs="$(printf '%s\n' "$sim" | awk '/^Remv /{print $2}')"

json_arr() {  # space/newline-separated names -> JSON string array
  local first=1 out="[" x
  for x in $1; do [ "$first" -eq 1 ] || out+=","; out+="\"$x\""; first=0; done
  printf '%s]' "$out"
}

printf '{"upgrade":%s,"remove":%s}\n' "$(json_arr "$insts")" "$(json_arr "$remvs")"
