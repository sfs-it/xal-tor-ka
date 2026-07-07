# ⛬ Xal-Tor-Ka · `beta0.4`

> 🌍 **Sprachen:** **Englisch (offiziell)** ([englische README](../../README.md)) ·
> [alle Sprachen](../README.md)

*Übersetzung des offiziellen englischen Dokuments; bei Abweichungen hat die englische Fassung Vorrang.*

**Ein einziger bewachter Eingang vor all Ihren Online-Diensten.** Xal-Tor-Ka ist der
"digitale Türsteher" Ihrer Infrastruktur: Er stellt eine einzige gehärtete Tür vor
jede Website und Anwendung, die Sie veröffentlichen, und entscheidet – für jede
einzelne –, wer eintreten darf. Keine über die einzelnen Dienste verstreuten
Passwörter mehr: Die Regel lebt an einem Ort.

Für eine **Entscheidungsträgerin oder einen Entscheidungsträger** ist der Nutzen
einfach: Sie veröffentlichen eine interne Anwendung, ohne sie der Welt
preiszugeben; Sie entscheiden, ob ein Dienst **für alle offen**, **angemeldeten
Benutzern vorbehalten** (mit Zwei-Faktor-Verifizierung) oder **nur ausdrücklich
autorisierten Personen** zugänglich ist; und Sie verwalten alles über ein
**geschütztes Web-Panel**. Benutzer können sich mit internen Zugangsdaten oder mit
ihrem **Google**- oder **Microsoft**-Firmenkonto anmelden. Das System führt ein
Zugriffsprotokoll, verteidigt sich gegen Brute-Force-Versuche und sichert seine
eigene Konfiguration.

> 👉 Wenn Sie nur wissen möchten, **was es tut und wie es das tut**, gehen Sie zu
> **[`TECHNOTES.md`](./TECHNOTES.md)** (technisch, aber lesbar).
> Wenn Sie es **installieren oder die Voraussetzungen prüfen** möchten, siehe
> **[`REQUIREMENTS.md`](../../REQUIREMENTS.md)** und **[`INSTALL.md`](../../INSTALL.md)**.

## Für wen es gedacht ist

- Sie betreiben mehrere Webdienste (Intranet, Geschäftsanwendungen, Dashboards,
  interne Tools) und möchten **einen einzigen kontrollierten Eingangspunkt**,
  anstatt jeden einzeln zu schützen.
- Sie möchten **zentral entscheiden**, wer was sieht, mit einem zweiten
  Sicherheitsfaktor und/oder Firmen-Anmeldung (Google/Microsoft), **ohne die
  einzelnen Dienste zu verändern**.
- Sie möchten, dass das einzige zum Internet hin exponierte Element ein **Schild**
  ist, während die eigentlichen Dienste dahinter verborgen bleiben.

## Was Sie erhalten

- **Eine einzige, gehärtete Tür**: Der Rest der Infrastruktur ist nicht direkt aus
  dem Internet erreichbar.
- **Drei Zugriffsstufen** pro Dienst: öffentlich · nur angemeldete Benutzer (2FA) ·
  nur Personen auf der Zulassungsliste.
- **Anmeldung mit Google / Microsoft** oder mit internen Zugangsdaten.
- **Ein geschütztes Web-Panel** zur Verwaltung von Diensten, Benutzern und
  Berechtigungen.
- **Betriebssicherheit**: Zugriffsprotokoll (anbindbar an
  Intrusion-Prevention-Systeme), automatische Konfigurations-Backups, Überwachung
  des Dienstzustands.

## Status

**`beta0.4`** — eine Vorabversion vor 1.0. Der Kern ist gebaut und funktioniert;
einige fortgeschrittene Funktionen werden noch verfeinert (siehe
[`TODO.md`](../../TODO.md)).

## Dokumentation

| Dokument | Zielgruppe | Inhalt |
|----------|----------|---------|
| **[TECHNOTES.md](./TECHNOTES.md)** | Interessierte / technische Prüfer | was es tut und **wie** |
| **[REQUIREMENTS.md](../../REQUIREMENTS.md)** | Ingenieure | was Sie zum Betrieb benötigen |
| **[INSTALL.md](../../INSTALL.md)** | Ingenieure | Schritt-für-Schritt-Installation (Docker und ohne Docker) |
| **[AUTH-PROVIDERS.md](../../AUTH-PROVIDERS.md)** | Ingenieure | Google / Microsoft / OIDC aktivieren |
| **[BLUEPRINT.md](../../BLUEPRINT.md)** | Entwickler | maßgebliche Architekturspezifikation |

Übersetzungen der Einstiegsdokumente (README, TECHNOTES) befinden sich unter
[`DOCS/`](../README.md). Die **englischen Versionen im Wurzelverzeichnis des
Repositorys sind offiziell**; im Falle von Abweichungen hat Englisch Vorrang.

## Name

**`Xal-Tor-Ka`**, wenn Sie *über* es sprechen (Marke, UI, Dokumentation);
**`xaltorka`**, wenn Sie *zu* ihm sprechen (Go-Modul, Binary, Docker-Dienst,
Hostname).

---

© 2026 **SFS.it di Zanutto Agostino** — vertrieben unter der
[Apache License 2.0](../../LICENSE).
