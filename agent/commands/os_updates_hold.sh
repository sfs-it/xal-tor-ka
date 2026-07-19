#!/bin/bash
# Hold or release ("no update") the named OS packages via apt-mark. A held package is
# excluded from upgrades until released. Params: XTK_P_ACTION = hold|unhold,
# XTK_P_PACKAGES = space-separated package names (re-validated here).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

action="${XTK_P_ACTION:-}"
case "$action" in hold|unhold) ;; *) echo "invalid action" >&2; exit 2 ;; esac

raw="${XTK_P_PACKAGES:-}"
pkgs=()
for t in $raw; do
  [[ "$t" =~ ^[a-z0-9][a-z0-9+.-]+$ ]] || { echo "invalid package name: $t" >&2; exit 2; }
  pkgs+=("$t")
done
[ "${#pkgs[@]}" -gt 0 ] || { echo "no packages" >&2; exit 2; }

apt-mark "$action" "${pkgs[@]}" >&2 || { echo "apt-mark $action failed" >&2; exit 1; }
printf '{"ok":true,"action":"%s","count":%s}\n' "$action" "${#pkgs[@]}"
