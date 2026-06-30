# Xal-Tor-Ka — Notes techniques (ce qu'il fait et comment)

*Traduction du document officiel en anglais ; en cas de divergence, la version anglaise prévaut.*

Une explication technique mais accessible de son fonctionnement. Pour la
**spécification rigoureuse** (schéma JSON, modèles de données, contrat des
endpoints), voir [`BLUEPRINT.md`](../../BLUEPRINT.md) ; pour l'installer, voir
[`REQUIREMENTS.md`](../../REQUIREMENTS.md) + [`INSTALL.md`](../../INSTALL.md).

## En une phrase

Xal-Tor-Ka est un **portier d'authentification + gestionnaire de reverse proxy** :
il place **NGINX** comme unique service exposé à internet, et pour chaque requête il
demande à un **service Go interne** (jamais exposé) s'il faut laisser passer,
demander la connexion ou refuser.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## Comment il décide : le flux `auth_request`

NGINX **ne connaît pas les règles**. Pour chaque requête entrante, il effectue une
*subrequest* interne vers l'endpoint `/validate` du service Go, en transmettant
l'hôte et le chemin d'origine. Le service répond avec un statut HTTP :

- **200** → NGINX poursuit avec `proxy_pass` vers le vrai backend ;
- **401** → NGINX redirige vers la page de connexion ;
- **403** → accès refusé.

Le principe est **fail-closed** : toute erreur, expiration de délai ou doute lors de
l'évaluation produit un 401/403, **jamais** un 200. Le service Go n'est pas joignable
depuis l'extérieur : seul NGINX, sur le réseau interne, peut l'interroger.

## Le cœur : la matrice d'autorisation

Pour chaque **hôte** et chaque **chemin**, l'une des trois règles s'applique :

| Règle | Qui entre |
|------|-------------|
| `public` | tout le monde |
| `authenticated` | utilisateurs avec une session valide et une **2FA** (TOTP) effectuée |
| `whitelist` | uniquement les utilisateurs **explicitement autorisés** pour ce service |

Les **administrateurs** peuvent tout consulter. La granularité est par sous-domaine
et par chemin (p. ex. `/` public mais `/admin` en whitelist sur le même hôte).

## Authentification

- **Local** : mots de passe hachés avec **argon2id** + un second facteur via **TOTP**
  (RFC 6238, compatible avec Google Authenticator/Authy).
- **OIDC** (OpenID Connect) : connexion déléguée à **Google**, **Microsoft/Entra**
  ou des fournisseurs génériques (**Keycloak, Authentik, Auth0, Okta, GitLab**). La
  signature du jeton d'identité est vérifiée par rapport aux clés publiques du
  fournisseur.
  - **Pas d'auto-provisionnement** : l'utilisateur doit déjà exister, déclaré pour ce
    fournisseur — se connecter avec Google ne suffit pas pour entrer. Voir
    [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md).
- **Sessions** : cookies `HttpOnly`/`SameSite=Lax` (et `Secure` derrière HTTPS),
  conservés en RAM avec persistance sur fichier (ils survivent à un redémarrage).

## Configuration : tout en JSON

Aucune base de données obligatoire : la configuration vit dans quelques fichiers JSON
typés, validés au démarrage (**Fail-Fast** : un champ inconnu ou une valeur hors plage
bloque le démarrage avec un message clair).

| Fichier | Contenu |
|------|---------|
| `config.json` | infrastructure (gabarit via variables d'environnement) : mode d'authentification, TLS, sessions, IP admin, fournisseurs |
| `secrets.json` | secrets (client secrets OIDC, jetons, SMTP) — jamais versionné |
| `users.json` | utilisateurs, rôles, 2FA, autorisations — jamais versionné |
| `services.json` | services gérés à l'exécution (backends en proxy + liens du tableau de bord) |

Les changements sur les utilisateurs/services s'appliquent **à chaud** (hot reload),
sans redémarrage.

## Composants (service Go)

Stdlib-first, binaire statique. Principaux packages :

- `handlers/` — endpoints HTTP : `/validate`, connexion + TOTP, callback OIDC, setup,
  le panneau `/admin`, le tableau de bord `/listing`.
- `providers/` — authentification : `local` et `oidc` (interface commune).
- `matrix/` — évaluation des règles d'autorisation (par hôte/chemin).
- `proxy/` — génère les vhosts backend de NGINX et recharge.
- `health/` — vérifications périodiques de l'état de santé des backends (endpoint `/health`).
- `config/` — chargement + validation + sauvegarde atomique avec snapshots.
- `audit/` — journal des tentatives d'accès échouées (pour fail2ban).
- `auth/` — hachage, TOTP, sessions, annuaire des utilisateurs.

## Reverse proxy : génération et rechargement

Le gestionnaire génère la configuration des backends NGINX (un `server{}` par hôte,
avec `auth_request` sur les routes protégées et `proxy_pass` vers l'upstream). Le
**rechargement** :

- **Docker** : le conteneur NGINX détecte le changement et se recharge lui-même
  (polling), car `inotify` n'est pas fiable sur les bind mounts de Docker
  Desktop/WSL2.
- **Host/LXD** : le service Go exécute une commande de rechargement configurable
  (`nginx -s reload` / `systemctl reload nginx`).

NGINX valide toujours la nouvelle configuration et, si elle est invalide, conserve
celle en cours d'exécution : une mauvaise régénération ne met pas le proxy hors
service.

## Gestion et exploitation

- **Panneau `/admin`** (restreint par IP) : gérer les services, les utilisateurs et
  les permissions, superviser l'état, dans des pages séparées.
- **Tableau de bord `/listing`** : montre à chaque utilisateur uniquement les
  services auxquels il a accès.
- **Onboarding** : la première exécution génère un jeton à expiration pour créer le
  premier administrateur via le web ; ensuite l'interface se verrouille.
- **Sauvegardes** : chaque enregistrement crée un snapshot avec auto-trash (conserve
  les N derniers), avec restauration également depuis la CLI.
- **Défense contre la force brute** : les tentatives échouées atterrissent dans un
  journal structuré (`logs/auth.log`) avec l'IP réelle du client, intégrable à
  **fail2ban**.

## La sécurité en un coup d'œil

- Le seul service exposé est **NGINX** ; le service Go est interne.
- **Fail-closed** sur tout le chemin d'autorisation.
- Comparaisons de secrets en **temps constant** ; les secrets ne sont **jamais**
  journalisés.
- Zone d'administration restreinte par **IP** ; l'IP réelle du client est tirée de
  `X-Forwarded-For` uniquement depuis des proxys de confiance.
- Le jeton de setup est à **usage unique** et à expiration.

## Déploiement

- **Docker Compose** (par défaut) : NGINX exposé, service Go interne, un sidecar en
  lecture seule pour la découverte des conteneurs, limites de ressources et rotation
  des logs.
- **Host / LXD / machine dédiée** : binaire statique + NGINX système, gouverné par
  trois variables (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`).
  Voir [`INSTALL.md`](../../INSTALL.md) §9.

## Version

Source unique de vérité dans `version/version.go` (ligne pré-1.0 `beta0.N`),
surchargeable au moment du build, et affichée dans `xaltorka version`, `/healthz`, le
journal de démarrage et l'interface.
