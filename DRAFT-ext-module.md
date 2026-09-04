# DRAFT — Il pattern riusabile "aggiungi un modulo `ext/`" a XalTorKa

> Canone ratificato con il secondo modulo (`ext/vpn`, dopo `ext/hosting`). Descrive COME
> si aggiunge un'estensione al gate senza toccare l'architettura del core. Ogni nuovo
> modulo (`ext/<mod>`) segue questi punti; se qualcosa qui non basta, si generalizza QUI,
> non si copia-incolla nel modulo. Fonte-esempi: `ext/hosting/`, `ext/vpn/`.

## Principio
Un modulo è la **terza corsia** del pattern a 3 tier: `core (Go, auth) → extension (UI, no-host) → agent (root, vettato)`. Il core **non conosce** il dominio del modulo: sa solo che esiste un upstream interno da reverse-proxare, gated dalla sessione admin. Il modulo è **node-agnostico** (nessun IP/hostname nel binario) e **inerte se non installato/abilitato** (un install nudo del core non cambia).

## I 5 file di `ext/<mod>/`
| file | ruolo |
|---|---|
| `main.go` | `package main` → binario/immagine `xtk-<mod>-ui`. Import `xaltorka/agent`+`version`+`xtkui`. Flag `-listen`/`-socket`/`-config` con default **generici** (mai node-specific). Route `/admin/<mod>[/]` (metodo+path Go1.22), `GET /healthz`→`ok`, `http.Server` coi 4 timeout. Render via `s.chrome(...).Render(...)`+`xtkui.LocParse`. **Nessun potere-host**: ogni mutazione via `callAgent` sul socket. |
| `Dockerfile` | multi-stage, **contesto = REPO ROOT** (`go build -o /xtk-<mod>-ui ./ext/<mod>`), `distroless/static-debian12:nonroot` (uid 65532), `EXPOSE 809x`. |
| `docker-compose.yml` | **overlay**: setta `<MOD>_UPSTREAM=http://xtk-<mod>-ui:809x` sul servizio `xaltorka` + definisce il servizio `xtk-<mod>-ui` (nonroot, `group_add`, reti, `deploy.resources.limits`+`logging` **obbligatori**, mount `:ro`/`:rw` per DIR non per file). |
| `README.md` | i 3 tier, l'integrazione, le pagine, le fasi. |
| `<pkg>/` | package Go della logica di dominio (⚠️ **non** chiamarlo come una dir-runtime ignorata — vedi trappola-naming). |

## Il wiring-core in 5 punti (l'unica modifica al core)
Per `<mod>` (es. `vpn`), gemello esatto di hosting:
1. **`handlers/server.go`** — campi `<Mod>Upstream string` + `<mod>Proxy *httputil.ReverseProxy`; e il blocco `if s.<Mod>Upstream != "" { NewSingleHostReverseProxy; mux.HandleFunc("/admin/<mod>"[+"/"], s.handle<Mod>Proxy) }` (**NO strip-prefisso**).
2. **`handlers/<mod>.go`** — `handle<Mod>Proxy`: 404 se upstream vuoto, poi **`s.adminSessionOK(w,r)`** (NON `adminGuard`: preserva il body POST), poi `ServeHTTP`.
3. **`main.go`** — `<Mod>Upstream: getenv("<MOD>_UPSTREAM", "")`.
4. **`xtkui.AdminNav(extra ...NavItem)`** — il core aggiunge un `NavItem` per estensione attiva (`handlers/admin.go`: `if s.<Mod>Upstream!=""{ mods=append(mods, NavItem{Key,Href,LabelKey}) }`); l'UI del modulo passa il proprio e lo marca `Active`. La firma **variadic** (non più `bool`) è ciò che rende l'aggiunta **una riga, non un parametro nuovo**.
5. **i18n** — chiave `admin.<mod>` nei **10** `i18n/locales/*.json`.
Il core resta INVARIATO se `<MOD>_UPSTREAM` è vuoto (404 + nav-nascosta).

## Dove vive la config del modulo — FUORI dal core
`config.json`→`models.Config` è **strict-decode** (`config/load.go` `DisallowUnknownFields`): una chiave nuova lì = campo Go nuovo = **rebuild + accoppiamento core↔modulo**. Quindi la config del modulo vive in un **file suo** (`<mod>.json`), posseduto e (se serve hot) ricaricato dall'extension. Hosting non ha sezione in config.json; vpn usa `vpn.json`. Regola: **il core sa solo dell'upstream**, mai del dominio del modulo.
- File montato per **DIRECTORY** (`./data/<mod>:/etc/xaltorka/<mod>:rw`), non per singolo-file → il save atomico (temp+rename) resta inode-stabile.
- Single-writer (solo l'extension scrive il suo file).

## Installazione node-agnostica
- **Binario:** solo flag/default generici — mai IP/hostname/DNS del nodo.
- **Overlay compose:** punta a un **nome-servizio docker** (`http://xtk-<mod>-ui:809x`), valido su ogni nodo.
- **Node-specific** (endpoint pubblico, subnet, cert, DNS, token) → env/flag dell'installer + `$XTK_*_ENV` fuori-repo (chmod 600), scritto a runtime nel `.env`/config del nodo. Mai nel binario né in config.json-core.
- **Installer** `deploy/<mod>/install.sh` (gemello di `deploy/agent/install.sh`): idempotente, `--repo` auto-derivato, build-in-container `golang:1.25` se manca `go`, e rende **`COMPOSE_FILE` sticky nel `.env`** — ⚠️ **con MERGE della lista** (`docker-compose.yml:ext/hosting/...:ext/<mod>/...`), MAI append-cieco (con più moduli, un append-cieco non aggiunge il secondo → regressione silente; cfr incidente 2026-07-17).
- **Deploy su un nodo remoto:** stage `deploy/hetzner/N0-<mod>.sh` gemello di `50-hosting.sh`, oppure — se il modulo è opzionale/org-specific — via **addon-slot** `XTK_ADDONS="N:/abs/N0-<mod>.sh"` (`deploy-all.sh`), che `source`-a `lib.sh`. Restart nginx **per ultimo** (l'overlay ricrea il core → nuovo IP docker → 502 se nginx cachea il vecchio).

## Convenzioni
- **Porte moduli:** `809x` incrementale — hosting `8090`, vpn `8091`, prossimo `8092`…
- **Nomi:** immagine/servizio `xtk-<mod>-ui`; env `<MOD>_UPSTREAM`; gid-socket agente dedicato per modulo (hosting 1997, vpn 1998).
- **⚠️ Trappola-naming (CHANGELOG certs→certmgr):** un package Go non deve avere il nome di una dir-runtime git/docker-ignored. Verifica `.gitignore`/`.dockerignore` PRIMA. Tieni lo stato-runtime sotto `data/<mod>/` (già ignorato) e dai al package un nome distinto (es. `vpnmgr`, non `vpn`).
- **Agent vettato:** riusa il binario `xtk-agent` con una **cartella-comandi dedicata** + socket/gid propri (manifest+script root:root 0755 esatto, fail-closed) invece di scrivere nuovo transport.

## Checklist "nuovo modulo"
- [ ] `ext/<mod>/{main.go,Dockerfile,docker-compose.yml,README.md,<pkg>/}` sul modello di `ext/vpn`.
- [ ] 5 punti di wiring-core (server/handlers/main/AdminNav/i18n) — build + `go test -race` no-regressione.
- [ ] config fuori dal core (`<mod>.json`, dir-mount `:rw`, single-writer).
- [ ] `deploy/<mod>/install.sh` (COMPOSE_FILE **merge**) + `Makefile <mod>-install:` + stage/addon deploy.
- [ ] porta `809x`, gid dedicato, package-name safe.
- [ ] inerte a default (`enabled=false` / upstream vuoto) — commit outward-safe.
