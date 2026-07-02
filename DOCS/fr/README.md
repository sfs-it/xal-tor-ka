# ⛬ Xal-Tor-Ka · `beta0.2`

*Traduction du document officiel en anglais ; en cas de divergence, la version anglaise prévaut.*

> 🌍 **Langues :** [README officiel en anglais](../../README.md) ·
> [toutes les langues](../README.md)

**Une seule entrée gardée devant tous vos services en ligne.** Xal-Tor-Ka est le
« portier numérique » de votre infrastructure : il place une unique porte renforcée
devant chaque site et application que vous publiez, et décide — pour chacun — qui
peut entrer. Fini les mots de passe éparpillés service par service : la règle vit en
un seul endroit.

Pour un **décideur**, la valeur est simple : vous publiez une application interne
sans l'exposer au monde ; vous choisissez si un service est **ouvert à tous**,
**réservé aux utilisateurs connectés** (avec vérification en deux étapes), ou
**réservé aux seules personnes explicitement autorisées** ; et vous gérez le tout
depuis un **panneau web protégé**. Les utilisateurs peuvent se connecter avec des
identifiants internes ou avec leur compte professionnel **Google** ou
**Microsoft**. Le système tient un journal des accès, se défend contre les
tentatives par force brute et sauvegarde sa propre configuration.

> 👉 Si vous voulez juste savoir **ce qu'il fait et comment il le fait**, allez à
> **[`TECHNOTES.md`](./TECHNOTES.md)** (technique mais accessible).
> Si vous devez l'**installer ou vérifier les prérequis**, voyez
> **[`REQUIREMENTS.md`](../../REQUIREMENTS.md)** et **[`INSTALL.md`](../../INSTALL.md)**.

## À qui il s'adresse

- Vous exploitez plusieurs services web (intranet, applications métier, tableaux de
  bord, outils internes) et vous voulez **un point d'entrée unique et contrôlé**
  plutôt que de protéger chacun séparément.
- Vous voulez **décider de manière centralisée** qui voit quoi, avec un second
  facteur de sécurité et/ou une connexion professionnelle (Google/Microsoft),
  **sans modifier les services individuels**.
- Vous voulez que la seule chose exposée à internet soit un **bouclier**, tandis que
  les vrais services restent cachés derrière lui.

## Ce que vous obtenez

- **Une porte unique et renforcée** : le reste de l'infrastructure n'est pas
  joignable directement depuis internet.
- **Trois niveaux d'accès** par service : public · utilisateurs connectés
  uniquement (2FA) · personnes inscrites sur liste d'autorisation uniquement.
- **Connexion avec Google / Microsoft** ou avec des identifiants internes.
- **Un panneau web protégé** pour gérer les services, les utilisateurs et les
  permissions.
- **Sécurité opérationnelle** : journal des accès (intégrable aux systèmes de
  prévention d'intrusion), sauvegardes automatiques de la configuration, supervision
  de l'état de santé des services.

## État

**`beta0.2`** — une version antérieure à la 1.0. Le cœur est construit et
fonctionnel ; certaines fonctionnalités avancées sont en cours de finition (voir
[`TODO.md`](../../TODO.md)).

## Documentation

| Document | Public visé | Contenu |
|----------|----------|---------|
| **[TECHNOTES.md](./TECHNOTES.md)** | curieux / évaluateurs techniques | ce qu'il fait et **comment** |
| **[REQUIREMENTS.md](../../REQUIREMENTS.md)** | ingénieurs | ce dont vous avez besoin pour l'exécuter |
| **[INSTALL.md](../../INSTALL.md)** | ingénieurs | installation pas à pas (avec et sans Docker) |
| **[AUTH-PROVIDERS.md](../../AUTH-PROVIDERS.md)** | ingénieurs | activer Google / Microsoft / OIDC |
| **[BLUEPRINT.md](../../BLUEPRINT.md)** | développeurs | spécification d'architecture faisant autorité |

Les traductions des documents d'entrée (README, TECHNOTES) se trouvent sous
[`DOCS/`](../README.md). Les **versions anglaises à la racine du dépôt font foi** ;
en cas de divergence, l'anglais prévaut.

## Nom

**`Xal-Tor-Ka`** quand vous parlez *de* lui (marque, interface, documentation) ;
**`xaltorka`** quand vous parlez *à* lui (module Go, binaire, service Docker, nom
d'hôte).

---

© 2026 **SFS.it di Zanutto Agostino** — distribué sous la
[licence Apache 2.0](../../LICENSE).
