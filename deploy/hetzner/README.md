# deploy/hetzner — autonomous Xal-Tor-Ka node install on Hetzner Cloud

Deterministic, idempotent scripts to reset an existing Hetzner Cloud VPS and deploy
the gateway stack from zero (base OS → Xal-Tor-Ka gate with a real Let's Encrypt
cert → hosting overlay), meant to be wired into the install/propagation tooling.

## Invariants
- **Dry-run by default.** Nothing destructive/remote runs without `--apply`
  (or `DRY_RUN=0`). Stage 0 still hits the API read-only — that is how a dry-run
  **verifies** the env against Hetzner without touching anything.
- **They groan on failure.** `set -euo pipefail` + an ERR trap printing the line.
- **No secret in the repo** (it is public): everything comes from the env, which
  lives OUTSIDE the repo (`XTK_HETZNER_ENV`).
- **Every fact is asserted**, not assumed (state read back from API / remote host).

## Prerequisite: env file (outside the repo)
Point `XTK_HETZNER_ENV` at the secrets file (`chmod 600`, never in git). Required keys
(template in the operator's blueprint `SECONDBRAIN/hetzner-deploy-blueprint.md §0`):
`HCLOUD_TOKEN`, `HCLOUD_SERVER_ID`, `HCLOUD_SERVER_IP`, `HCLOUD_SSH_KEY_NAME`,
plus optional `XTK_DOMAIN`, `XTK_ACME_EMAIL`, `XTK_ACME_STAGING`, `XTK_ADMIN_CIDR`,
`XTK_ADMIN_EMAIL`/`XTK_ADMIN_PASSWORD`.

## Stages (slots 0..9)
| # | script            | what it does                                     | destructive |
|---|-------------------|--------------------------------------------------|:-----------:|
| 0 | `00-preflight.sh` | validate env + verify token/server/image via API; generate the SSH key | no |
| 1 | `10-provision.sh` | register SSH key; **rebuild** Ubuntu (WIPE); inject the deploy key via rescue | **yes** |
| 2 | `20-baseos.sh`    | apt upgrade; Docker+compose+git+make; firewall 22/80/443 | yes (remote) |
| 3 | *(addon slot)*    | optional, env-declared — see below               | — |
| 4 | `40-xaltorka.sh`  | install the gate (INSTALL.md: clone→.env→up→ACME cert→seed admin) | yes (remote) |
| 5 | `50-hosting.sh`   | build+install the hosting agent + overlay; nginx restart last | yes (remote) |

## Optional addons (org-specific, outside this repo)
Site/org-specific provisioning (private repos, OS users, …) is kept OUT of this public
repo. Declare addons in the machine env, choosing the slot they run at:
```
XTK_ADDONS="3:/abs/path/to/my-addon.sh"        # runs at slot 3 (after base OS)
```
An addon is a standalone script (outside the repo) that sources `lib.sh` via
`$XTK_HETZNER_LIB` (exported by `deploy-all.sh`) and uses the same helpers
(`ssh_run`, `load_env`, …).

## Usage
```bash
export XTK_HETZNER_ENV=/path/to/secrets/hetzner-<id>.env
./deploy-all.sh                 # dry-run of everything (verification included)
./deploy-all.sh --only 0        # just preflight (read-only)
./deploy-all.sh --apply         # execute all slots (rebuild included)
./deploy-all.sh --from 4 --apply  # resume from the gate install
```
On the maintainer's machines wrap the whole run for audit: `run+log -- ./deploy-all.sh --apply`.
