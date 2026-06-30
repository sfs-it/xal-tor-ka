# Xal-Tor-Ka — Technische Hinweise (was es tut und wie)

*Übersetzung des offiziellen englischen Dokuments; bei Abweichungen hat die englische Fassung Vorrang.*

Eine technische, aber lesbare Erläuterung, wie es funktioniert. Für die **rigorose
Spezifikation** (JSON-Schema, Datenmodelle, Endpoint-Vertrag) siehe
[`BLUEPRINT.md`](../../BLUEPRINT.md); zur Installation siehe [`REQUIREMENTS.md`](../../REQUIREMENTS.md)
+ [`INSTALL.md`](../../INSTALL.md).

## In einem Satz

Xal-Tor-Ka ist ein **Authentifizierungs-Gatekeeper + Reverse-Proxy-Manager**: Er
stellt **NGINX** als einzigen zum Internet exponierten Dienst voran, und für jede
Anfrage fragt er einen **internen Go-Dienst** (niemals exponiert), ob er
durchlassen, zum Login auffordern oder verweigern soll.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## Wie er entscheidet: der `auth_request`-Ablauf

NGINX **kennt die Regeln nicht**. Für jede eingehende Anfrage stellt es eine
interne *Subrequest* an den `/validate`-Endpoint des Go-Dienstes und übergibt dabei
den ursprünglichen Host und Pfad. Der Dienst antwortet mit einem HTTP-Status:

- **200** → NGINX fährt mit `proxy_pass` zum echten Backend fort;
- **401** → NGINX leitet zur Login-Seite weiter;
- **403** → Zugriff verweigert.

Das Prinzip ist **fail-closed**: Jeder Fehler, jedes Timeout oder jeder Zweifel
während der Auswertung führt zu 401/403, **niemals** zu einem 200. Der Go-Dienst ist
von außen nicht erreichbar: Nur NGINX kann ihn im internen Netzwerk abfragen.

## Der Kern: die Autorisierungsmatrix

Für jeden **Host** und **Pfad** gilt eine von drei Regeln:

| Regel | Wer hereinkommt |
|------|-------------|
| `public` | jeder |
| `authenticated` | Benutzer mit gültiger Sitzung und abgeschlossener **2FA** (TOTP) |
| `whitelist` | nur Benutzer, die für diesen Dienst **ausdrücklich autorisiert** sind |

**Administratoren** können auf alles zugreifen. Die Granularität gilt pro
Subdomain und pro Pfad (z. B. `/` öffentlich, aber `/admin` als whitelist auf
demselben Host).

## Authentifizierung

- **Local**: Passwörter gehasht mit **argon2id** + ein zweiter Faktor über **TOTP**
  (RFC 6238, kompatibel mit Google Authenticator/Authy).
- **OIDC** (OpenID Connect): Login delegiert an **Google**, **Microsoft/Entra** oder
  generische Provider (**Keycloak, Authentik, Auth0, Okta, GitLab**). Die Signatur
  des Identitätstokens wird gegen die öffentlichen Schlüssel des Providers geprüft.
  - **Kein Auto-Provisioning**: Der Benutzer muss bereits existieren und für diesen
    Provider deklariert sein — sich mit Google anzumelden reicht nicht aus, um
    hereinzukommen. Siehe [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md).
- **Sitzungen**: `HttpOnly`/`SameSite=Lax`-Cookies (und `Secure` hinter HTTPS), im
  RAM gehalten mit Dateipersistenz (sie überleben einen Neustart).

## Konfiguration: alles in JSON

Keine zwingende Datenbank: Die Konfiguration lebt in einigen typisierten
JSON-Dateien, die beim Start validiert werden (**Fail-Fast**: ein unbekanntes Feld
oder ein Wert außerhalb des zulässigen Bereichs blockiert den Start mit einer klaren
Meldung).

| Datei | Inhalt |
|------|---------|
| `config.json` | Infrastruktur (env-templated): Authentifizierungsmodus, TLS, Sitzungen, Admin-IPs, Provider |
| `secrets.json` | Geheimnisse (OIDC-Client-Secrets, Tokens, SMTP) — niemals versioniert |
| `users.json` | Benutzer, Rollen, 2FA, Autorisierungen — niemals versioniert |
| `services.json` | zur Laufzeit verwaltete Dienste (proxied Backends + Dashboard-Links) |

Änderungen an Benutzern/Diensten werden **heiß** angewendet (Hot Reload), ohne
Neustart.

## Komponenten (Go-Dienst)

Stdlib-first, statisches Binary. Hauptpakete:

- `handlers/` — HTTP-Endpoints: `/validate`, Login + TOTP, OIDC-Callback, Setup,
  das `/admin`-Panel, das `/listing`-Dashboard.
- `providers/` — Authentifizierung: `local` und `oidc` (gemeinsame Schnittstelle).
- `matrix/` — Auswertung der Autorisierungsregeln (pro Host/Pfad).
- `proxy/` — generiert die NGINX-Backend-Vhosts und lädt neu.
- `health/` — periodische Backend-Zustandsprüfungen (`/health`-Endpoint).
- `config/` — Laden + Validierung + atomares Speichern mit Snapshots.
- `audit/` — Protokoll fehlgeschlagener Zugriffsversuche (für fail2ban).
- `auth/` — Hashing, TOTP, Sitzungen, Benutzerverzeichnis.

## Reverse Proxy: Generierung und Reload

Der Manager generiert die NGINX-Backend-Konfiguration (ein `server{}` pro Host, mit
`auth_request` auf geschützten Routen und `proxy_pass` zum Upstream). Der
**Reload**:

- **Docker**: Der NGINX-Container erkennt die Änderung und lädt sich selbst neu
  (Polling), weil `inotify` auf Bind-Mounts unter Docker Desktop/WSL2 unzuverlässig
  ist.
- **Host/LXD**: Der Go-Dienst führt einen konfigurierbaren Reload-Befehl aus
  (`nginx -s reload` / `systemctl reload nginx`).

NGINX validiert stets die neue Konfiguration und behält, falls sie ungültig ist, die
laufende bei: Eine fehlerhafte Neugenerierung legt den Proxy nicht lahm.

## Verwaltung und Betrieb

- **`/admin`-Panel** (IP-beschränkt): Verwaltung von Diensten, Benutzern und
  Berechtigungen, Statusüberwachung, in getrennten Seiten.
- **`/listing`-Dashboard**: zeigt jedem Benutzer nur die Dienste, auf die er
  zugreifen kann.
- **Onboarding**: Der erste Start generiert ein ablaufendes Token, um den ersten
  Administrator über das Web zu erstellen; danach riegelt sich die Oberfläche ab.
- **Backups**: Jedes Speichern erstellt einen Snapshot mit Auto-Trash (behält die
  letzten N), mit Wiederherstellung auch über die CLI.
- **Brute-Force-Abwehr**: Fehlgeschlagene Versuche landen in einem strukturierten
  Protokoll (`logs/auth.log`) mit der echten Client-IP, anbindbar an **fail2ban**.

## Sicherheit auf einen Blick

- Der einzige exponierte Dienst ist **NGINX**; der Go-Dienst ist intern.
- **Fail-closed** über den gesamten Autorisierungspfad.
- Vergleiche von Geheimnissen in **konstanter Zeit**; Geheimnisse werden **niemals**
  protokolliert.
- Der Admin-Bereich ist per **IP** beschränkt; die echte Client-IP wird nur von
  vertrauenswürdigen Proxys aus `X-Forwarded-For` übernommen.
- Das Setup-Token ist **einmalig verwendbar** und läuft ab.

## Deployment

- **Docker Compose** (Standard): NGINX exponiert, Go-Dienst intern, ein
  schreibgeschützter Sidecar für die Container-Discovery, Ressourcenlimits und
  Log-Rotation.
- **Host / LXD / dedizierte Maschine**: statisches Binary + System-NGINX, gesteuert
  durch drei Variablen (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`).
  Siehe [`INSTALL.md`](../../INSTALL.md) §9.

## Version

Einzige Quelle der Wahrheit in `version/version.go` (Vor-1.0-Linie `beta0.N`), zur
Build-Zeit überschreibbar, und angezeigt in `xaltorka version`, `/healthz`, dem
Startprotokoll und der UI.
