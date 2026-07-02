# ⛬ Xal-Tor-Ka · `beta0.2`

> 🌍 **Lingua:** **English (ufficiale)** → [../../README.md](../../README.md) ·
> tutte le lingue / all languages → [../README.md](../README.md)

*Traduzione del documento ufficiale in inglese; in caso di difformità, prevale la versione inglese.*

**Un unico ingresso sorvegliato davanti a tutti i tuoi servizi online.** Xal-Tor-Ka è il
"portiere digitale" della tua infrastruttura: mette un'unica porta blindata davanti a
ogni sito e applicazione che pubblichi e decide — per ciascuno — chi può entrare.
Niente più password sparse servizio per servizio: la regola vive in un solo posto.

Per chi **decide**, il valore è semplice: pubblichi un'applicazione interna senza
esporla al mondo; scegli se un servizio è **aperto a tutti**, **riservato agli utenti
autenticati** (con verifica in due passaggi) oppure **solo per le persone esplicitamente
autorizzate**; e gestisci tutto da un **pannello web protetto**. Gli utenti possono
accedere con credenziali interne o con il loro account aziendale **Google** o
**Microsoft**. Il sistema tiene un registro degli accessi, si difende dai tentativi di
forza bruta e fa il backup della propria configurazione.

> 👉 Se vuoi solo sapere **cosa fa e come lo fa**, vai a
> **[`TECHNOTES.md`](./TECHNOTES.md)** (tecnico ma leggibile).
> Se devi **installarlo o verificare i prerequisiti**, consulta
> **[`REQUIREMENTS.md`](../../REQUIREMENTS.md)** e **[`INSTALL.md`](../../INSTALL.md)**.

## A chi è rivolto

- Gestisci diversi servizi web (intranet, applicazioni aziendali, dashboard, strumenti
  interni) e vuoi **un unico punto di ingresso controllato** invece di proteggere
  ciascuno separatamente.
- Vuoi **decidere centralmente** chi vede cosa, con un secondo fattore di sicurezza
  e/o accesso aziendale (Google/Microsoft), **senza modificare i singoli servizi**.
- Vuoi che l'unica cosa esposta su internet sia uno **scudo**, mentre i servizi
  reali restano nascosti dietro di esso.

## Cosa ottieni

- **Un'unica porta blindata**: il resto dell'infrastruttura non è raggiungibile
  direttamente da internet.
- **Tre livelli di accesso** per servizio: pubblico · solo utenti autenticati (2FA) ·
  solo persone in allow-list.
- **Accesso con Google / Microsoft** o con credenziali interne.
- **Un pannello web protetto** per gestire servizi, utenti e permessi.
- **Sicurezza operativa**: registro degli accessi (integrabile con sistemi di
  prevenzione delle intrusioni), backup automatici della configurazione, monitoraggio
  dello stato dei servizi.

## Stato

**`beta0.2`** — una release pre-1.0. Il nucleo è costruito e funzionante; alcune
funzionalità avanzate sono in fase di rifinitura (vedi [`TODO.md`](../../TODO.md)).

## Documentazione

| Documento | Destinatari | Contenuto |
|----------|----------|---------|
| **[TECHNOTES.md](./TECHNOTES.md)** | curiosi / valutatori tecnici | cosa fa e **come** |
| **[REQUIREMENTS.md](../../REQUIREMENTS.md)** | tecnici | cosa serve per eseguirlo |
| **[INSTALL.md](../../INSTALL.md)** | tecnici | installazione passo passo (con e senza Docker) |
| **[AUTH-PROVIDERS.md](../../AUTH-PROVIDERS.md)** | tecnici | abilitare Google / Microsoft / OIDC |
| **[BLUEPRINT.md](../../BLUEPRINT.md)** | sviluppatori | specifica architetturale autoritativa |

Le traduzioni dei documenti d'ingresso (README, TECHNOTES) si trovano sotto
[`DOCS/`](../README.md). Le **versioni inglesi nella root del repository sono
ufficiali**; in caso di discrepanza, prevale l'inglese.

## Nome

**`Xal-Tor-Ka`** quando ne parli *come marchio* (brand, UI, documentazione);
**`xaltorka`** quando ci parli *direttamente* (modulo Go, binario, servizio Docker,
hostname).

---

© 2026 **SFS.it di Zanutto Agostino** — distribuito sotto
[Apache License 2.0](../../LICENSE).
