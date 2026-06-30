# Xal-Tor-Ka — Notas técnicas (qué hace y cómo)

*Traducción del documento oficial en inglés; en caso de cualquier discrepancia, prevalece la versión en inglés.*

Una explicación técnica pero legible de cómo funciona. Para la **especificación
rigurosa** (esquema JSON, modelos de datos, contrato de endpoints) consulta
[`BLUEPRINT.md`](../../BLUEPRINT.md); para instalarlo, consulta [`REQUIREMENTS.md`](../../REQUIREMENTS.md)
+ [`INSTALL.md`](../../INSTALL.md).

## En una frase

Xal-Tor-Ka es un **gatekeeper de autenticación + gestor de reverse proxy**: coloca
**NGINX** como único servicio expuesto a internet, y para cada solicitud pregunta a un
**servicio Go interno** (nunca expuesto) si debe dejar pasar, pedir login o denegar.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## Cómo decide: el flujo `auth_request`

NGINX **no conoce las reglas**. Para cada solicitud entrante realiza una *subrequest*
interna al endpoint `/validate` del servicio Go, pasándole el host y la ruta originales.
El servicio responde con un estado HTTP:

- **200** → NGINX continúa con `proxy_pass` hacia el backend real;
- **401** → NGINX redirige a la página de login;
- **403** → acceso denegado.

El principio es **fail-closed**: cualquier error, timeout o duda durante la evaluación
produce 401/403, **nunca** un 200. El servicio Go no es accesible desde el exterior:
solo NGINX, en la red interna, puede consultarlo.

## El núcleo: la matriz de autorización

Para cada **host** y **ruta**, se aplica una de tres reglas:

| Regla | Quién entra |
|------|-------------|
| `public` | cualquiera |
| `authenticated` | usuarios con sesión válida y **2FA** (TOTP) completado |
| `whitelist` | solo usuarios **explícitamente autorizados** para ese servicio |

Los **administradores** pueden acceder a todo. La granularidad es por subdominio y por
ruta (p. ej. `/` público pero `/admin` en whitelist en el mismo host).

## Autenticación

- **Local**: contraseñas con hash mediante **argon2id** + un segundo factor vía **TOTP**
  (RFC 6238, compatible con Google Authenticator/Authy).
- **OIDC** (OpenID Connect): login delegado a **Google**, **Microsoft/Entra** o
  proveedores genéricos (**Keycloak, Authentik, Auth0, Okta, GitLab**). La firma del
  token de identidad se verifica contra las claves públicas del proveedor.
  - **Sin auto-aprovisionamiento**: el usuario debe existir ya, declarado para ese
    proveedor — iniciar sesión con Google no basta para entrar. Consulta
    [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md).
- **Sesiones**: cookies `HttpOnly`/`SameSite=Lax` (y `Secure` detrás de HTTPS),
  mantenidas en RAM con persistencia en archivo (sobreviven a un reinicio).

## Configuración: todo en JSON

Sin base de datos obligatoria: la configuración vive en unos pocos archivos JSON
tipados, validados al arranque (**Fail-Fast**: un campo desconocido o un valor fuera de
rango bloquea el arranque con un mensaje claro).

| Archivo | Contenido |
|------|---------|
| `config.json` | infraestructura (con plantillas de variables de entorno): modo de auth, TLS, sesiones, IPs de admin, proveedores |
| `secrets.json` | secretos (client secrets de OIDC, tokens, SMTP) — nunca versionado |
| `users.json` | usuarios, roles, 2FA, autorizaciones — nunca versionado |
| `services.json` | servicios gestionados en runtime (backends proxiados + enlaces del panel) |

Los cambios en usuarios/servicios se aplican **en caliente** (hot reload), sin reinicio.

## Componentes (servicio Go)

Stdlib-first, binario estático. Paquetes principales:

- `handlers/` — endpoints HTTP: `/validate`, login + TOTP, callback OIDC, setup,
  el panel `/admin`, el panel `/listing`.
- `providers/` — autenticación: `local` y `oidc` (interfaz común).
- `matrix/` — evaluación de las reglas de autorización (por host/ruta).
- `proxy/` — genera los vhosts de backend de NGINX y recarga.
- `health/` — comprobaciones periódicas de salud de los backends (endpoint `/health`).
- `config/` — carga + validación + guardado atómico con snapshots.
- `audit/` — registro de intentos de acceso fallidos (para fail2ban).
- `auth/` — hashing, TOTP, sesiones, directorio de usuarios.

## Reverse proxy: generación y recarga

El gestor genera la configuración de backend de NGINX (un `server{}` por host, con
`auth_request` en las rutas protegidas y `proxy_pass` hacia el upstream). La **recarga**:

- **Docker**: el contenedor de NGINX detecta el cambio y se recarga a sí mismo (polling),
  porque `inotify` no es fiable en los bind mounts de Docker Desktop/WSL2.
- **Host/LXD**: el servicio Go ejecuta un comando de recarga configurable
  (`nginx -s reload` / `systemctl reload nginx`).

NGINX siempre valida la nueva configuración y, si es inválida, mantiene la que está en
ejecución: una regeneración incorrecta no tumba el proxy.

## Gestión y operaciones

- **Panel `/admin`** (restringido por IP): gestiona servicios, usuarios y permisos,
  monitoriza el estado, en páginas separadas.
- **Panel `/listing`**: muestra a cada usuario solo los servicios a los que puede acceder.
- **Onboarding**: el primer arranque genera un token con caducidad para crear el primer
  administrador vía web; después la interfaz se blinda.
- **Copias de seguridad**: cada guardado crea un snapshot con auto-trash (conserva los
  últimos N), con restauración también desde la CLI.
- **Defensa contra fuerza bruta**: los intentos fallidos se registran en un log
  estructurado (`logs/auth.log`) con la IP real del cliente, integrable en **fail2ban**.

## Seguridad de un vistazo

- El único servicio expuesto es **NGINX**; el servicio Go es interno.
- **Fail-closed** en todo el camino de autorización.
- Comparaciones de secretos en **tiempo constante**; los secretos **nunca** se registran.
- Área de admin restringida por **IP**; la IP real del cliente se toma de
  `X-Forwarded-For` solo desde proxies de confianza.
- El token de setup es de **un solo uso** y con caducidad.

## Despliegue

- **Docker Compose** (por defecto): NGINX expuesto, servicio Go interno, un sidecar de
  solo lectura para el descubrimiento de contenedores, límites de recursos y rotación de logs.
- **Host / LXD / máquina dedicada**: binario estático + NGINX del sistema, gobernado por
  tres variables (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`).
  Consulta [`INSTALL.md`](../../INSTALL.md) §9.

## Versión

Fuente única de verdad en `version/version.go` (línea pre-1.0 `beta0.N`),
sobrescribible en tiempo de compilación, y mostrada en `xaltorka version`, `/healthz`, el
log de arranque y la interfaz.
