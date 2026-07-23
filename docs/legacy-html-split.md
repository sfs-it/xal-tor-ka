# Lo split `legacy-html` — architettura, principio, worklist

> Autore: **Custode-11**, 2026-07-23 (notte), su mandato di Mannie. Contesto organizzativo:
> `~/IA/PROJECTS/xaltorka/SECONDBRAIN/01-variazione-progetto_team-e-legacy-html.md`.
> Questo documento è tecnico: **come** il frontend HTML è stato diviso dal core, e **come si finisce**.

## Perché
Il progetto passa a un team a figure. Il pannello, oggi **server-rendered in Go**, viene isolato in un
layer nominato **`legacy-html`** così che:
- **`dev-go-html-fe`** possa mantenerlo senza toccare la logica di business;
- **Custode (dev-go-be)** possieda il **core backend + il contratto dell'API**;
- lo **stesso core** serva domani anche l'Ajax-a-frammenti e un'eventuale SPA — senza riscrivere la logica.

## Il principio che rende lo split SICURO (leggilo prima di continuare)
`html/template` lega i dati **per nome-campo via reflection** (`Execute(w, data any)`): **il testo di un
template non ha accoppiamento di tipo Go con la struct che renderizza.** Conseguenza operativa enorme:

> **Spostare un template è spostare TESTO.** Le struct dati restano dove sono (negli handler), invariate.
> L'output è **identico per costruzione**; il compilatore becca ogni rottura strutturale; gli occhi confermano.

Non è un refactor rischioso: è un **move meccanico verificabile in tre modi** (build+vet+test · corpi
byte-identici · occhi).

## L'architettura (il seam)
```
Richiesta ──▶ handlers/*.go                         ──▶ risposta
              (parse → valida → SERVIZIO → RENDER)
                                   │            │
                                   │            └─▶ legacyhtml.<Name>Tmpl   (HTML, oggi)
                                   │                 [futuro] fragment HTML  (Ajax)
                                   │                 [futuro] JSON           (SPA)
                                   ▼
                              backend/service (config.Load*/Save*, s.mutate*)
                              = il CORE-API, corsia dev-go-be
```
- **`legacyhtml/`** (nuovo package) = **solo template** (il *view-layer*). Corsia **dev-go-html-fe**.
- **`handlers/`** = **logica**: parse richiesta → chiama il servizio → **sceglie la risposta**. Corsia mista:
  il *routing/parse* è be, ma la *scelta di render HTML* è il punto di contatto col fe.
- **`xtkui/`** = la **chrome condivisa** (doctype/topbar/nav/subtab, `Chrome.Render`) + gli **asset**
  (`assets/admin.css`, `admin.js`) + gli helper template (`LocParse`, `LocFuncs`, `ruleOptions`). Condivisa
  con l'estensione; resta com'è.
- **Il core-API** (`config.LoadServices/SaveServices`, `LoadUsers/SaveUsers`, `SaveSecrets`, `s.mutateServices`,
  `s.mutateUsers`) **esiste già** ed è separato dall'HTML: è la base su cui il layer JSON si appoggerà.

## Cosa è FATTO (2026-07-23, commit `1f8a712`)
I **12 template del pannello admin** spostati in `legacyhtml/`, riferimenti aggiornati, `admin.go`
1911 → 1514 righe:

| Template (esportato) | Pagina | Da |
|---|---|---|
| `OverviewTmpl` | `/admin` | admin.go |
| `ServicesTmpl` | `/admin/servizi` | admin.go |
| `AdminEditTmpl` | `/admin/backend/edit` (editor 4 tab) | admin.go |
| `UsersTmpl` · `UserDetailTmpl` | `/admin/utenti` · `/admin/utenti/{email}` | admin.go |
| `MonitoringTmpl` | `/admin/monitoring` | admin.go |
| `DockerTmpl` · `HostScanTmpl` | `/admin/docker` · `/admin/hostscan` | admin.go |
| `AdminQRTmpl` | QR TOTP admin | admin.go |
| `ProvidersTmpl` · `ProviderEditTmpl` | `/admin/providers` · `/admin/provider/edit` | providers.go |
| `TlsTmpl` | `/admin/tls` | tls.go |

**Verifiche:** `go build`+`vet` ok · **13/13 pacchetti test verdi** · i 12 corpi **byte-identici** al
pre-split (provato con diff normalizzato su `git HEAD`) · pagine **pixel-identiche** su localhost (occhi).

## Cosa è FATTO — batch A: pagine-intere auth (2026-07-23, commit `d4611c8`)
8 template pagina-intera (proprio `<!doctype>`, non passano per `Chrome.Render`) spostati in
`legacyhtml/*_fullpage_tmpl.go`: `LoginTmpl`, `TotpTmpl` (login.go) · `ProfileTmpl`, `ProfileQRTmpl`
(profile.go) · `CodeRequestTmpl`, `CodeVerifyTmpl` (otp.go) · `ListingTmpl` (server.go) ·
`ForbiddenAdminTmpl` (adminauth.go). Verificati: build+vet+13 test · **8 corpi byte-identici** a HEAD ·
login **pixel-identica** su localhost (occhi).

## Cosa RESTA (worklist per `dev-go-html-fe` — stesso pattern, stesso rischio ~zero)

### A) Le due pagine di setup (`handlers/setup.go`) — l'unico wrinkle
`setupCredTmpl`, `setupTOTPTmpl`: **concatenano il var `setupHead`** (`Parse(setupHead + ` … `)`), quindi
sposta **anche `setupHead`** in `legacyhtml` (o esponilo) prima del move. Lasciate a parte apposta: è il
flusso d'installazione, raramente toccato, e non voglio l'edge-case del concat fatto di corsa di notte.

**Ricetta (per ognuno):** taglia il blocco `var xTmpl = template.Must(...)`, incollalo in
`legacyhtml/<file>_tmpl.go` col nome **esportato** (`XTmpl`), aggiorna i riferimenti negli handler a
`legacyhtml.XTmpl`, togli l'import `xtkui`/`html/template` se rimane inutilizzato nell'handler, `go build`.

### B) L'estensione hosting (`ext/hosting/main.go`, 1893 righe)
È un **binario separato** con i propri template (~10, il pannello Hosting). Stesso principio, ma **package
proprio** (es. `ext/hosting/legacyhtml/` o un `views.go` dedicato) — non condivide il modulo del core.
Da fare in un secondo tempo, non blocca il core.

## Il contratto dell'API (corsia dev-go-be, il prossimo passo del CORE)
Il seam sopra rende l'API una **razionalizzazione**, non una riscrittura. Per ogni operazione:
- **input** (oggi form-encoded / multipart per l'upload immagine) → **service-call** (già esiste) → **risposta**.
- **Mutazioni (~28):** `POST /admin/backend/{add,edit,del,toggle}`, `/admin/user/{add,del,admin,authz,email,password,totp}`,
  `/admin/tls/{issue,renew,del}`, `/admin/provider/{add,edit,del,toggle,test}`, `/admin/link/{add,del,toggle}`,
  `/admin/monitor/{add,del}`, `/admin/discover/add`, `/admin/hostscan/add`, `/admin/adminips`, `/admin/reload`.
- **Letture (~13):** i `GET /admin/*`.
- **Da disegnare:** un contratto JSON coerente (request/response) + **errori strutturati** (oggi `http.Error`
  con stringa). **Auth:** same-origin (pannello attuale + Ajax) usa il **cookie che c'è già → zero nuova
  superficie**; solo una **SPA embeddabile** in un'app client separata costa **token + CORS** (l'unico costo
  di sicurezza, e solo se/quando la si vuole).

## Regola d'oro per chi continua
1. **Un template alla volta**, `go build` dopo ognuno (tree sempre verde).
2. **Prova che è un MOVE**: il corpo del template dev'essere **byte-identico** al pre-split (nessun ritocco
   di contenuto nello stesso commit dello spostamento — separa move e restyle).
3. **Occhi**: screenshot prima/dopo su localhost (`~/xtk-shot/sonda-capture.js`), devono coincidere.
4. **Niente deploy-prod senza il «vai» di Mannie.**
