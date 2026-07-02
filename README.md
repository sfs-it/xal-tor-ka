# ⛬ Xal-Tor-Ka · `beta0.2`

> 🌍 **Languages:** **English (official)** ·
> [Italiano](DOCS/it/README.md) · [Français](DOCS/fr/README.md) ·
> [Español](DOCS/es/README.md) · [Deutsch](DOCS/de/README.md) ·
> [Русский](DOCS/ru/README.md) · [Português](DOCS/pt/README.md) ·
> [中文](DOCS/zh/README.md) · [हिन्दी](DOCS/hi/README.md) ·
> [العربية](DOCS/ar/README.md) — see [`DOCS/`](DOCS/README.md)

**One guarded entrance in front of all your online services.** Xal-Tor-Ka is the
"digital doorman" of your infrastructure: it puts a single hardened door in front of
every site and application you publish, and decides — for each one — who may enter.
No more passwords scattered service by service: the rule lives in one place.

For a **decision-maker**, the value is simple: you publish an internal application
without exposing it to the world; you choose whether a service is **open to
everyone**, **reserved for signed-in users** (with two-step verification), or
**only for explicitly authorized people**; and you manage everything from a
**protected web panel**. Users can sign in with internal credentials or with their
corporate **Google** or **Microsoft** account. The system keeps an access log,
defends itself against brute-force attempts, and backs up its own configuration.

> 👉 If you just want to know **what it does and how it does it**, go to
> **[`TECHNOTES.md`](TECHNOTES.md)** (technical but readable).
> If you need to **install it or check the prerequisites**, see
> **[`REQUIREMENTS.md`](REQUIREMENTS.md)** and **[`INSTALL.md`](INSTALL.md)**.

## Who it is for

- You run several web services (intranet, business apps, dashboards, internal
  tools) and want **a single controlled entry point** instead of protecting each
  one separately.
- You want to **decide centrally** who sees what, with a second security factor
  and/or corporate sign-in (Google/Microsoft), **without modifying the individual
  services**.
- You want the only thing exposed to the internet to be a **shield**, while the
  real services stay hidden behind it.

## What you get

- **A single, hardened door**: the rest of the infrastructure is not reachable
  directly from the internet.
- **Three access levels** per service: public · signed-in users only (2FA) ·
  allow-listed people only.
- **Sign in with Google / Microsoft** or with internal credentials.
- **A protected web panel** to manage services, users and permissions.
- **Operational security**: access log (pluggable into intrusion-prevention
  systems), automatic configuration backups, service health monitoring.

## Status

**`beta0.2`** — a pre-1.0 release. The core is built and working; some advanced
features are being polished (see [`TODO.md`](TODO.md)).

## Documentation

| Document | Audience | Content |
|----------|----------|---------|
| **[TECHNOTES.md](TECHNOTES.md)** | curious / technical evaluators | what it does and **how** |
| **[REQUIREMENTS.md](REQUIREMENTS.md)** | engineers | what you need to run it |
| **[INSTALL.md](INSTALL.md)** | engineers | step-by-step install (Docker and non-Docker) |
| **[AUTH-PROVIDERS.md](AUTH-PROVIDERS.md)** | engineers | enable Google / Microsoft / OIDC |
| **[BLUEPRINT.md](BLUEPRINT.md)** | developers | authoritative architecture spec |

Translations of the entry documents (README, TECHNOTES) live under
[`DOCS/`](DOCS/README.md). The **English versions in the repository root are
official**; in case of any discrepancy, English prevails.

## Name

**`Xal-Tor-Ka`** when you talk *about* it (brand, UI, docs); **`xaltorka`** when you
talk *to* it (Go module, binary, Docker service, hostname).

---

© 2026 **SFS.it di Zanutto Agostino** — distributed under the
[Apache License 2.0](LICENSE).
