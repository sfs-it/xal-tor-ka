# Struttura di un sito multi-vhost (hosting Xal-Tor-Ka)

> **Fonte autoritativa** del layout di un sito hosting su disco e della sua mappatura Docker +
> gateway. Codifica la convenzione **Plesk-like** decisa da Mannie (2026-07-20): pensata per
> **rendere facile la vita a chi ci mette le mani** — un tecnico che apre `/opt/sites/<sito>/`
> deve capire *al volo* dove sta il dominio principale, dove i secondari, dove i log.
> I **template dell'agente** (`site_create`, `vhost_create`) generano ESATTAMENTE questa struttura;
> se cambi la convenzione, cambi il template — mai a mano sui file generati (`# do not edit by hand`).

## 1. Principio (perché così)

Un **sito** = un utente di sistema (`site-<slug>`, gruppo `docker-hosting`) + una cartella
`/opt/sites/<slug>/`. Dentro possono vivere **più vhost**: un **dominio primario** (la vetrina, il
sito «vero») e zero o più **domini/sottodomini aggiuntivi** (`app.`, `api.`, `staging.`…), ciascuno
in una **docker dedicata** (isolamento forte: il costo di una docker in più è irrisorio rispetto a
un blocco di sistema — dottrina di stirpe dal Fondatore).

La regola d'oro del layout: **il primario è "in chiaro" nella root del sito; gli aggiuntivi sono
sottostrutture nominate.** Come Plesk: apri la cartella e il sito principale è lì, gli altri hanno
il loro nome. Niente `www/` anonimo, niente `.vhosts` da decifrare per capire cosa serve cosa.

## 2. Layout su disco — `/opt/sites/<slug>/`

```
/opt/sites/<slug>/
├── httpdocs/            ← DOCROOT del DOMINIO PRIMARIO (la vetrina). Ci carichi via SCP.
├── <vhost>/             ← DOCROOT di ogni dominio AGGIUNTIVO, uno per cartella (es. app/, api/).
├── logs/                ← LOG del dominio PRIMARIO (access.log/error.log DIRETTI qui).
│   └── <vhost>/         ← LOG di ogni aggiuntivo, in sottocartella (logs/app/, logs/api/…).
├── .vhosts/             ← config GENERATE (root-owned, non-editabili a mano):
│   ├── httpdocs/{docker-compose.yml, nginx.conf, .xtk-stack}
│   └── <vhost>/{docker-compose.yml, nginx.conf, .xtk-stack}
└── .ssh/                ← chiavi autorizzate per l'accesso SCP/SFTP del sito (chroot).
```

**Regola dei nomi (Plesk-like):**
| Cosa | Dominio PRIMARIO | Domini AGGIUNTIVI |
|---|---|---|
| Docroot | `httpdocs/` | `<vhost>/` (es. `app/`, `api/`) |
| Log | `logs/` (file diretti) | `logs/<vhost>/` |
| Vhost id | `httpdocs` | `<vhost>` |

**Ownership:** i **docroot e i log** sono `site-<slug>:docker-hosting` (l'utente ci scrive via SCP e
la docker ci gira dentro col suo uid). Le **config** in `.vhosts/` e la root del sito sono
`root:root 0755` (necessario per il chroot SFTP e perché sono generate/vettate, non toccabili
dall'utente).

## 3. Mappatura Docker (una docker per vhost)

Ogni vhost ha un `docker-compose.yml` in `.vhosts/<vhost>/`, avviato con
`--project-directory /opt/sites/<slug>` (così `./httpdocs`, `./logs`… risolvono dalla root del sito).

- **Immagine** per template: `NGINX (static)` → `nginx-unprivileged`; `NGINX + PHP-FPM` → nginx +
  container php-fpm; `+ Apache`, `+ node` come da template.
- **Volumi** (esempio, vhost primario static):
  - docroot: `./httpdocs:/var/www/html:ro`  ·  aggiuntivo: `./<vhost>:/var/www/html:ro`
  - nginx: `./.vhosts/<vhost>/nginx.conf:/etc/nginx/conf.d/default.conf:ro`
  - log: `./logs:/var/log/nginx`  ·  aggiuntivo: `./logs/<vhost>:/var/log/nginx`
- **Rete**: unica esterna **`xtk-hosting`**; **nessuna porta pubblicata sull'host** (il gateway
  raggiunge la docker solo via rete interna).
- **Alias di rete** (come il gateway trova la docker) — convenzione:
  | Vhost | Alias docker |
  |---|---|
  | primario (`httpdocs`) | `<slug>.site` |
  | aggiuntivo | `<slug>-<vhost>.site` |
  Il gateway fa reverse-proxy verso `http://<alias>:8080`.
- **Limiti**: `deploy.resources.limits` (es. `cpus 0.25`, `memory 64M` per un static) + logging
  json-file con rotazione.

## 4. Mappatura Gateway (pubblicazione + TLS)

- **Publish** (dal pannello Hosting o «Servizi»): crea un **backend** in `services.json`
  con `host=<dominio>`, `route.upstream=http://<alias>:8080`, `rule` (public/authenticated/authorized).
  Il gateway rigenera `nginx/conf.d/backends.conf` → il dominio è servito.
- **TLS**: dalla pagina «Certificati» → «emetti LE» (ACME HTTP-01: il dominio deve risolvere
  pubblicamente su questo gateway con la :80 raggiungibile) o «self-signed» (CA interna, per
  LAN/dev). Il cert è `<dominio>.crt/.key` nella dir certs montata.
- **Match stato nel pannello**: Publish/SSL si agganciano al backend per **upstream** (l'alias del
  vhost) **e** per **host** (il dominio), e il cert si cerca sul **dominio** — così lo stato non si
  «perde» se un dominio è pubblicato con un upstream diverso.

## 5. Perché questa convenzione (la spiegazione che mi hai chiesto)

1. **Leggibilità immediata (Plesk-like).** Chi apre `/opt/sites/segnalapa/` deve vedere `httpdocs/`
   (il sito) e `app/`, `api/`… (gli altri), non un `www/` anonimo. Idem i log: `logs/*.log` è il
   primario, `logs/app/` è app. È il modello mentale che ogni sistemista già ha da Plesk/cPanel.
2. **Sana un'incoerenza reale del codice.** Prima di questa regola il primario usava docroot `www/`
   ma la UI diceva «carica in `httpdocs/`»: due nomi per la stessa cosa → confusione (è *esattamente*
   ciò che ha generato il pasticcio segnalapa, dove la vetrina è finita in una docker standalone
   ad-hoc fuori struttura). Un solo nome canonico, `httpdocs/`, chiude l'ambiguità.
3. **Perché allineo anche i TEMPLATE dell'agente (non solo segnalapa).** «Senza porcate» significa
   che la struttura giusta la genera il template, non una mano che edita un file `# do not edit`.
   Se cambiassi solo i file di segnalapa, il prossimo `site_create` ricreerebbe il vecchio `www/` →
   la convenzione sarebbe una bugia sul disco. Quindi la regola vive nel template: i siti **futuri**
   nascono già `httpdocs/ + logs/`, e la migrazione di un sito legacy è un'operazione una-tantum
   documentata (§6).

## 6. Migrare un sito legacy/ad-hoc alla struttura (checklist)

Per un sito che non segue la convenzione (es. `www/` invece di `httpdocs/`, o una docker standalone
fuori dai `.vhosts/`):
1. **Contenuto**: sposta il docroot del primario in `httpdocs/` (owner `site-<slug>`), i log in
   `logs/`. Gli aggiuntivi restano `<vhost>/` + `logs/<vhost>/`.
2. **Config**: rigenera i `.vhosts/<vhost>/` dal template aggiornato (docroot `./httpdocs`, log
   `./logs`, alias corretto) — non editare a mano i file generati.
3. **Docker**: avvia i container puliti (`vhost start`), verifica che l'alias serva il contenuto
   (curl interno) **prima** di ritirare la vecchia docker standalone.
4. **Publish/TLS**: verifica il backend (dominio → alias del vhost) e ri-emetti il cert se serve.
5. **Pulizia**: ritira la docker standalone e rimuovi le cartelle ad-hoc fuori convenzione **per
   ultime**, solo dopo aver verificato che il vhost pulito serve.

---
*Redatto da Custode-6, 2026-07-20 — su convenzione di Mannie. Se cambi la convenzione, aggiorna
questo doc E i template dell'agente insieme: sono la stessa verità in due forme.*
