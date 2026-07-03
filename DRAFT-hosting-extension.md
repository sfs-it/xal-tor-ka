# DESIGN — Estensione "hosting" + framework estensioni (in corso)

> Documento di design (versionato). Nato dalla discussione 2026-07-03: trasformare
> Xal-Tor-Ka in una piccola **piattaforma (core + estensioni)** senza intaccare la
> sicurezza del gateway internet-facing. Fase 1 (`xtkui`) già implementata; questo
> resta la traccia per contratto d'estensione + agente.

## Principio di sicurezza (il perno)
Il **pericolo** esiste solo se il gateway esposto acquisisce il potere di
creare/avviare container o gestire utenti/DB. Regola: **il ciclo di vita e i
privilegi restano FUORI dal core.** Il core resta read-only su Docker, low-priv.

## Tre livelli (privilegi separati)
1. **System agent** (host, systemd) — l'UNICO con superpoteri: `docker up/down`,
   crea utenti OS, admin DB. API **stretta, autenticata, internal-only**, verbi
   whitelisted e parametrici (niente shell libera), **audit** di ogni azione.
   Nessuna esposizione web. Perimetro critico minimizzato.
   - Trasporto (DECISO): **unix socket sull'host** di default (massimo contenimento;
     auth via permessi del socket, gruppo dedicato). La **porta TCP** (mTLS su rete
     interna) è un'**alternativa abilitabile in un secondo momento**, gated da
     **whitelist** IP — off di default. Agente su host via **systemd**, non in
     container (evita docker.sock montato in un container).
2. **Estensione hosting** (nuova Docker + sw) — logica + UI di management
   (crea sito / gestisci DB / gestisci utenti-app). **Non ha poteri propri**:
   chiede tutto all'agente. Sta **dietro** il gateway (auth-gated), non internet-facing.
3. **Xal-Tor-Ka core** (gateway) — INVARIATO. Se l'estensione non è installata,
   **zero rischio aggiunto**.

## "Stessa interfaccia" → SDK condivisa `xtkui`
Estrarre design system (Sentinel) + framework i18n in un package condiviso
`xtkui`, importato sia dal core sia dalle estensioni → look/dark-mode/10 lingue/
componenti nativi. Apre un vero **framework di estensioni** (hosting è la prima).

## I 3 contratti d'integrazione col core
- **Auth/SSO:** l'estensione vive dietro il gateway → `auth_request` la protegge →
  riceve `X-Auth-User`. Nessun login separato; eredita utenti/ruoli del core.
- **Proxy + cert:** l'estensione (via agente) accende un sito → chiede al core di
  aggiungere il **backend** + emettere il **cert** (API/CLI già pronti: `cert issue`,
  mutateServices). L'estensione orchestra, il core proxa e certifica.
- **UI:** voce di nav / pannello proxato, stile identico via `xtkui`.

## Difesa in profondità
Se buchi l'estensione: l'agente ha API narrow → danno max = "le operazioni
definite", non RCE. Il core internet-facing è un layer più in là, senza quei poteri.

## Layout monorepo (module `xaltorka`, più binari)
```
/                      core gateway (main.go a root, invariato)
  handlers/ proxy/ certmgr/ config/ ...   (core)
  xtkui/               NEW: design system + i18n condivisi (estratti da handlers/i18n)
  agent/               NEW: system agent (package main → binario host/systemd)
  ext/hosting/         NEW: estensione hosting (package main → binario/container)
  deploy/agent/        NEW: unit systemd dell'agente
  docker-compose.hosting.yml  overlay opt-in (estensione + wiring), profilo compose
```
- Estensione **opt-in**: compose separato / profilo → `docker compose up` (core) NON
  la avvia; si attiva esplicitamente.
- Runtime dell'hosting (volumi siti, DB) **gitignored** come certs/secrets.

## Isolamento sito (DECISO)
- Compose **self-contained per-sito** in `/opt/sites/<nome>/`.
- **Utente OS reale per-sito** (no utenti virtuali: scartati). Ogni sito ha un utente
  di sistema dedicato, tutti nel gruppo **non privilegiato `docker-hosting`**
  (`nologin`, no gruppo `docker`, no sudo). L'agente:
  - `user create` → crea l'utente host reale in `docker-hosting`, **`chown`** del
    mount del sito, e avvia il container **come quel UID/GID** (`user: "uid:gid"` nel
    compose, o PUID/PGID). → i file scritti nei bind-mount fuori dalla docker
    **corrispondono** all'utente in-container (fpm/python) **e** restano isolati
    per-sito (l'uid del sito A non tocca i file del sito B sull'host).
  - Il `db create` è separato: il DB ha il suo volume dati, non il mount file del sito.

## DB: engine + topologia (selezionabili per-sito)
Due assi indipendenti, scelti per-sito:

**Engine** — alcuni progetti richiedono **PostgreSQL**, altri **MariaDB/MySQL**:
- **MariaDB/MySQL** (`mariadb`/`mysql`) e **PostgreSQL** (`postgres`, incl. varianti
  tipo `postgis` per il GIS).

**Topologia** — dedicato vs condiviso:
- **Dedicato**: container DB **nel compose del sito** (isolamento a livello container,
  ma un'istanza DB per sito → più risorse). Per app che vogliono la loro istanza.
- **Condiviso**: una (o più) **istanza DB condivisa** persistente sulla rete interna,
  usata da più siti hosting. Per sito l'agente crea **database + utente dedicati**
  sull'istanza condivisa (isolamento a livello db/role, non container) → efficiente,
  niente N container DB. Possibili **più istanze condivise** (per engine e/o per
  gruppo di fiducia). *Caveat:* isolamento più debole del dedicato.

**Agente engine-aware** (stessi verbi, driver diverso):
- pg → `createdb` / `CREATE ROLE … LOGIN`; mysql → `CREATE DATABASE` / `CREATE USER`.
- dedicato → l'engine è nel compose del sito; condiviso → provisioning `db+user`
  sull'istanza condivisa + iniezione creds nell'app.
- Il pannello mostra: **engine (pg|mysql) × topologia (dedicato|condiviso→scegli istanza)**.
- Credenziali generate come **secret** (mai in git/log), iniettate nel container app
  via env. Porta DB **mai esposta** (solo rete interna, regole Docker §1).

## Fasatura
1. **Base che serve comunque:** estrarre `xtkui` (design+i18n) + definire il
   **contratto d'estensione** (registrazione nav/proxy/auth) + **protocollo agente**
   (verbi, auth, audit).
2. **Estensione hosting** come prima implementazione: template `nginx+php-fpm`
   con DB **a scelta MariaDB o PostgreSQL**, agente `site up/down` ·
   `db create (engine)` · `user create`, integrazione backend/cert.

## Decisioni chiuse
- Trasporto agente: **unix socket** (default) + TCP/mTLS opzionale con whitelist, abilitabile dopo.
- Isolamento sito: **utente OS reale per-sito** nel gruppo `docker-hosting`.
- Struttura: **dir nel repo** (monorepo), non repo a sé.
- Engine DB: pg **o** mysql, per-sito. Topologia: dedicato **o** condiviso, per-sito.

## Aperti (in fase di implementazione)
- Formato messaggi agente (JSON verbi request/response) + formato audit.
- Set esatto dei template di sito (php-fpm+nginx primo).
