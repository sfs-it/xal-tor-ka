#!/bin/bash
# Apply upgrades for ONLY the named OS packages (upgrade-only — never installs new
# packages — and NEVER reboots). Param XTK_P_PACKAGES = space-separated package names.
# The agent already validated the param against the manifest pattern; here we re-validate
# each token (defense in depth) before handing them to apt. apt output goes to stderr
# (captured/audited by the agent); a JSON summary goes to stdout on success.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

raw="${XTK_P_PACKAGES:-}"
[ -n "$raw" ] || { echo "no packages given" >&2; exit 2; }

pkgs=()
for t in $raw; do
  [[ "$t" =~ ^[a-z0-9][a-z0-9+.-]+$ ]] || { echo "invalid package name: $t" >&2; exit 2; }
  pkgs+=("$t")
done
[ "${#pkgs[@]}" -gt 0 ] || { echo "no valid packages" >&2; exit 2; }

# --only-upgrade: upgrade the named packages if installed; never pull in a brand-new one.
apt-get install --only-upgrade -y "${pkgs[@]}" >&2 || { echo "apt upgrade failed" >&2; exit 1; }

reboot=false
[ -f /var/run/reboot-required ] && reboot=true
printf '{"ok":true,"applied":%s,"reboot_required":%s}\n' "${#pkgs[@]}" "$reboot"
