# ⛬ Xal-Tor-Ka · `beta0.1`

*Traducción del documento oficial en inglés; en caso de cualquier discrepancia, prevalece la versión en inglés.*

> 🌍 **Idiomas:** [English (oficial)](../../README.md) · [todos los idiomas](../README.md)

**Una única entrada vigilada delante de todos tus servicios en línea.** Xal-Tor-Ka es el
"portero digital" de tu infraestructura: coloca una sola puerta blindada delante de
cada sitio y aplicación que publicas, y decide —para cada uno— quién puede entrar.
Se acabaron las contraseñas dispersas servicio por servicio: la regla vive en un solo lugar.

Para quien **toma decisiones**, el valor es simple: publicas una aplicación interna
sin exponerla al mundo; eliges si un servicio está **abierto a todos**, **reservado a
usuarios autenticados** (con verificación en dos pasos), o **solo para personas
explícitamente autorizadas**; y gestionas todo desde un **panel web protegido**. Los
usuarios pueden iniciar sesión con credenciales internas o con su cuenta corporativa
de **Google** o **Microsoft**. El sistema mantiene un registro de accesos, se defiende
frente a intentos de fuerza bruta y realiza copias de seguridad de su propia configuración.

> 👉 Si solo quieres saber **qué hace y cómo lo hace**, ve a
> **[`TECHNOTES.md`](./TECHNOTES.md)** (técnico pero legible).
> Si necesitas **instalarlo o comprobar los requisitos previos**, consulta
> **[`REQUIREMENTS.md`](../../REQUIREMENTS.md)** e **[`INSTALL.md`](../../INSTALL.md)**.

## Para quién es

- Gestionas varios servicios web (intranet, aplicaciones de negocio, paneles,
  herramientas internas) y quieres **un único punto de entrada controlado** en lugar
  de proteger cada uno por separado.
- Quieres **decidir de forma centralizada** quién ve qué, con un segundo factor de
  seguridad y/o inicio de sesión corporativo (Google/Microsoft), **sin modificar los
  servicios individuales**.
- Quieres que lo único expuesto a internet sea un **escudo**, mientras los servicios
  reales permanecen ocultos detrás de él.

## Qué obtienes

- **Una única puerta blindada**: el resto de la infraestructura no es accesible
  directamente desde internet.
- **Tres niveles de acceso** por servicio: público · solo usuarios autenticados (2FA) ·
  solo personas en lista de permitidos.
- **Inicio de sesión con Google / Microsoft** o con credenciales internas.
- **Un panel web protegido** para gestionar servicios, usuarios y permisos.
- **Seguridad operativa**: registro de accesos (integrable en sistemas de prevención
  de intrusiones), copias de seguridad automáticas de la configuración, monitorización
  del estado de los servicios.

## Estado

**`beta0.1`** — una versión previa a la 1.0. El núcleo está construido y funcionando;
algunas funciones avanzadas se están puliendo (consulta [`TODO.md`](../../TODO.md)).

## Documentación

| Documento | Audiencia | Contenido |
|----------|----------|---------|
| **[TECHNOTES.md](./TECHNOTES.md)** | evaluadores curiosos / técnicos | qué hace y **cómo** |
| **[REQUIREMENTS.md](../../REQUIREMENTS.md)** | ingenieros | qué necesitas para ejecutarlo |
| **[INSTALL.md](../../INSTALL.md)** | ingenieros | instalación paso a paso (con Docker y sin Docker) |
| **[AUTH-PROVIDERS.md](../../AUTH-PROVIDERS.md)** | ingenieros | habilitar Google / Microsoft / OIDC |
| **[BLUEPRINT.md](../../BLUEPRINT.md)** | desarrolladores | especificación de arquitectura autoritativa |

Las traducciones de los documentos de entrada (README, TECHNOTES) están bajo
[`DOCS/`](../README.md). Las **versiones en inglés en la raíz del repositorio son
oficiales**; en caso de cualquier discrepancia, prevalece el inglés.

## Nombre

**`Xal-Tor-Ka`** cuando hablas *acerca* de él (marca, interfaz, documentación);
**`xaltorka`** cuando hablas *con* él (módulo Go, binario, servicio Docker, nombre de host).

---

© 2026 **SFS.it di Zanutto Agostino** — distribuido bajo la
[Apache License 2.0](../../LICENSE).
