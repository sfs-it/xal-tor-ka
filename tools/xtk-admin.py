#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 SFS.it di Zanutto Agostino
"""xtk-admin — local management CLI for XalTorKa gateways.

Thin, dependency-free (stdlib-only) wrapper around the `xaltorka` binary's admin
subcommands, run INSIDE the gateway container (locally via `docker exec`, or over
SSH to a fleet box). Optionally audited through `run+log`.

First capability — **admin-cidr**: manage the admin IP whitelist (who may reach
`/admin`). This is the fix for the lockout a stale/narrow ADMIN_CIDR causes: it
edits `services.json` admin_ip_whitelist from the box (no web UI, no need to be
already whitelisted) and, with --apply, hot-reloads via SIGHUP (no restart).

Examples:
  # open /admin on a remote box and hot-apply, audited via run+log
  xtk-admin.py admin-cidr open  --host 10.0.0.118 --key ~/.ssh-vps/id_ed25519 --apply --runlog
  # set an explicit whitelist locally (this machine runs the container)
  xtk-admin.py admin-cidr set 203.0.113.7/32 10.0.0.0/24 --local --apply
  # inspect the effective whitelist on the Hetzner node
  xtk-admin.py admin-cidr show --host 157.180.94.155 --key ~/IA/.../hetzner_ed25519
"""
import argparse
import shlex
import subprocess
import sys


def build_onbox_cmd(a, xtk_args):
    """The command to run inside the container: xaltorka admin-cidr ... [-config] [--apply]."""
    inner = [a.bin, "admin-cidr", *xtk_args, "-config", a.config]
    if a.apply:
        inner.append("--apply")
    cmd = ["docker", "exec", a.container, *inner]
    if a.runlog:
        cmd = ["run+log", "--project", "xtk-admin", *cmd]
    return cmd


def main():
    p = argparse.ArgumentParser(prog="xtk-admin", description="XalTorKa gateway management.")
    sub = p.add_subparsers(dest="group", required=True)

    ac = sub.add_parser("admin-cidr", help="manage the admin IP whitelist (who reaches /admin)")
    ac.add_argument("op", choices=["show", "set", "add", "open", "clear"])
    ac.add_argument("cidr", nargs="*", help="CIDR(s)/IP(s) for set/add")
    ac.add_argument("--host", help="SSH host of the box (omit with --local)")
    ac.add_argument("--user", default="root", help="SSH user (default: root)")
    ac.add_argument("--key", help="SSH private key path")
    ac.add_argument("--local", action="store_true", help="run on THIS machine (no SSH)")
    ac.add_argument("--container", default="xaltorka-xaltorka-1", help="gateway container name")
    ac.add_argument("--config", default="/etc/xaltorka", help="config dir inside the container")
    ac.add_argument("--bin", default="xaltorka", help="gateway binary name in the container")
    ac.add_argument("--apply", action="store_true", help="hot-apply via SIGHUP (no restart)")
    ac.add_argument("--runlog", action="store_true", help="wrap the on-box command in run+log (audit)")
    ac.add_argument("--dry-run", action="store_true", help="print the command, do not run it")

    a = p.parse_args()

    if a.op in ("set", "add") and not a.cidr:
        p.error(f"admin-cidr {a.op} requires at least one CIDR/IP")
    if not a.local and not a.host:
        p.error("need --host <ssh-host> or --local")

    onbox = build_onbox_cmd(a, [a.op, *a.cidr])
    if a.local:
        cmd = onbox
    else:
        ssh = ["ssh"]
        if a.key:
            ssh += ["-i", a.key]
        ssh += ["-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=10",
                f"{a.user}@{a.host}"]
        # one argument = the remote command line, safely quoted
        cmd = ssh + [" ".join(shlex.quote(x) for x in onbox)]

    print("→ " + " ".join(shlex.quote(x) for x in cmd), file=sys.stderr)
    if a.dry_run:
        return 0
    return subprocess.call(cmd)


if __name__ == "__main__":
    sys.exit(main())
