# Laravel su Xal-Tor-Ka — scelta d'architettura + valutazione di un template ready-to-use

> Autore: Custode-7 · 2026-07-21 · contesto: provisioning `api-staging.segnalapa.it` (demo Laravel che scrive su pgsql).
> Correlati: `site-structure.md` (convenzione multisite), `case-study-segnalapa.md`.

## 1. La scelta fatta per `api-staging.segnalapa.it`

Sito multi-vhost `segnalapa` (utente OS `site-segnalapa`, chroot SCP porta 2222). Il vhost
`api-staging` è stato adattato a **Laravel** riusando lo stack `php-fpm` esistente, senza inventare
nulla di nuovo:

- **Stack**: `php-fpm 8.3` — `web` (nginxinc/nginx-unprivileged) + `php` (php-fpm) su rete privata
  per-vhost; solo `web` è sulla rete `xtk-hosting` così il gateway fa reverse-proxy.
- **Webroot Laravel** = `public/`. Unica modifica rispetto al template php-fpm: la `nginx.conf` del
  vhost ha `root /var/www/html/public` (invece di `/var/www/html`) + front-controller
  (`try_files $uri $uri/ /index.php?$query_string`), applicata via il comando vettato `vhost_nginx_set`
  (validato con `nginx -t`, reload). `public/` creata e owned dal site-user.
- **Database**: 2 DB pgsql **isolati** creati col comando vettato `db_create` (engine=pg) sul
  `xtk-db-pg` condiviso — `segnalapa_auth_staging` e `segnalapa_core_staging`, ciascuno con un ruolo
  che possiede **solo il proprio DB** (nessun cross-grant). **PostGIS dormiente** (immagine
  `postgis 16-3.4`: disponibile, si attiva con `CREATE EXTENSION postgis` sul core quando serve).
  Host dal container: `xtk-db-pg.db:5432` (rete `xtk-hosting`).
- **Credenziali**: NON passate via bottiglia. Depositate nel chroot in
  `api-staging/.db-staging.env` (0600, owned site-user, **fuori** dal webroot `public/`) + copia
  locale in `.secrets/segnalapa-staging-db.env` nella casa dell'agente. Il dev le cabla nel `.env`
  Laravel.
- **Deploy del codice**: SCP nel chroot dentro `api-staging/` (in modo che esista
  `api-staging/public/index.php`). Poi `composer install` + `php artisan migrate`.

Tutte le mutazioni sul VPS via comandi **vettati** (`db_create`, `vhost_nginx_set`) sul socket
dell'agente, dentro `run+log` (audit `#199`–`#203`).

## 2. Il limite trovato — **bloccante** (immagine PHP stock)

Il template `php-fpm` usa l'immagine **stock** `php:<ver>-fpm-alpine`. Verifica dal vivo sul php di
`api-staging`:

```
php -m  →  mbstring, PDO, pdo_sqlite, tokenizer      # e poco altro
```

**Mancano le estensioni che Laravel richiede**, in particolare **`pdo_pgsql`** (driver postgres):
qualsiasi app Laravel su pgsql fallisce con *«could not find driver»*. Mancano anche `pdo_mysql`,
`bcmath`, `gd`, `zip`, `intl`, `opcache`, `pcntl` (queue), `redis`.

Finora i vhost `php-fpm` in produzione erano di fatto **statici/vetrina** (nessun Laravel+pg aveva
ancora girato — i sorgenti prod erano attesi) → **il gap era latente** ed è emerso ora con la prima
app che scrive davvero. **Conseguenza operativa immediata**: `api-staging` non eseguirà `migrate`
finché l'immagine php non ha `pdo_pgsql`.

## 3. Valutazione — template `laravel` ready-to-use, auto-mantenuto

### 3.1 Fattibilità: **sì**, con un precedente già in casa
`nginx/Dockerfile` (immagine WAF ModSecurity+CRS, Custode-5) è già un'**immagine custom buildata al
deploy** (`docker compose build`). Un'immagine `php` con le estensioni Laravel segue lo **stesso
pattern collaudato** — niente di nuovo a livello di meccanica.

### 3.2 Design proposto (`agent/templates/laravel/`)
- **`Dockerfile`**: `FROM php:8.3-fpm-alpine` + `install-php-extensions`
  (mlocati/docker-php-extension-installer) per `pdo_pgsql pdo_mysql bcmath gd zip intl opcache pcntl
  redis` **+ composer**. (Variante per 8.2/8.1 col medesimo Dockerfile parametrico.)
- **`nginx.conf`**: `root .../public` + front-controller (già scritto per api-staging → si canonizza).
- **`docker-compose.yml`**: `web` + `php` (immagine custom) **+ opzionali** `queue` (`php artisan
  queue:work`) e `scheduler` (loop `php artisan schedule:run` ogni 60s). Tutti col medesimo mount del
  docroot.
- **Registrazione stack**: aggiungere l'opzione `laravel:8.3` (e 8.2/8.1) al dropdown in
  `ext/hosting/main.go` (dove ora ci sono `php-fpm:8.x`).
- **First-boot (opt-in)**: entrypoint che, alla prima salita, fa `composer install --no-dev` +
  `php artisan key:generate` + `php artisan migrate --force` — dietro un flag, perché tocca stato.

### 3.3 Limiti / condizioni (onesti — vanno decisi prima di costruire)
1. **Estensioni PHP** → risolto dal build custom (è il cuore del template). ✅
2. **`auto_update` è solo un flag, senza esecutore.** `site_autoupdate.sh`/`vhost_autoupdate.sh`
   scrivono `auto_update=` in `.xtk-stack`; **nessun processo lo consuma** (verificato: nessun
   watcher/cron/watchtower legge il flag). Quindi *"docker pronta e mantenuta in automatico"* richiede
   una **feature net-new**: un esecutore periodico che, per i vhost con `auto_update=true`, faccia
   `docker compose build/pull → up -d → healthcheck → rollback` (riusando i tag `*:rollback-*` già in
   uso al deploy). **Per un'immagine custom (laravel) "update" = rebuild** (ripull base php + reinstall
   estensioni), più pesante di un semplice pull di tag pubblicato → conviene schedularlo (es. notturno)
   e gated.
3. **Composer / migrazioni NON in auto.** `composer update` (dipendenze applicative) e `migrate`
   (stato del DB) **non** vanno automatizzati: sono responsabilità dell'app-owner o di un deploy
   assistito. L'auto-manutenzione si limita agli **aggiornamenti di sicurezza dell'immagine base**.
4. **Risorse.** Il cap attuale (`256M`/`0.5 cpu` per il php) è stretto per Laravel + queue/scheduler →
   alzare o rendere configurabile nel template.
5. **`.env` / naming DB.** `db.env` è iniettato come `env_file` del container → Laravel legge
   `env('DB_*')`. Per un template "ready" conviene **allineare le chiavi** a quelle di Laravel
   (`DB_CONNECTION=pgsql`, `DB_HOST/PORT/DATABASE/USERNAME/PASSWORD`) o generare un `.env` scaffold.
   *(Nota: per api-staging ho usato chiavi `AUTH_*`/`CORE_*` perché l'app ha due connessioni; il dev
   le mappa sui propri connection-name.)*

### 3.4 Cosa significa "auto-mantenuta", realisticamente
- **Fattibile e sensato**: rebuild periodico *gated* dell'immagine base (patch di sicurezza php/nginx)
  + healthcheck + rollback automatico. È l'estensione naturale del flag `auto_update` (che oggi non fa
  nulla) verso un vero esecutore.
- **Da NON automatizzare**: dipendenze applicative (composer) e migrazioni — restano all'app-owner.

## 4. Raccomandazione
1. **Costruire `agent/templates/laravel/`** (immagine custom con le estensioni + `nginx.conf` public/
   + compose): è **piccolo, additivo, provabile su localhost** — e **risolve subito `api-staging`**
   diventando al contempo il template riusabile per ogni futura app Laravel.
2. **Auto-manutenzione**: trattarla come **feature separata** (l'esecutore del flag `auto_update`), non
   parte del template — è net-new e vale per *tutti* gli stack, non solo Laravel.
3. **Immediato / sblocco demo**: `api-staging` ha bisogno dell'immagine con `pdo_pgsql` **prima** che
   il `migrate` del TL funzioni. Finché non c'è, avvisare il TL di non lanciare `migrate`.
