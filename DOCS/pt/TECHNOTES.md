# Xal-Tor-Ka — Notas técnicas (o que ele faz e como)

*Tradução do documento oficial em inglês; em caso de divergência, prevalece a versão em inglês.*

Uma explicação técnica, mas legível, de como funciona. Para a **especificação
rigorosa** (esquema JSON, modelos de dados, contrato de endpoints) veja
[`BLUEPRINT.md`](../../BLUEPRINT.md); para instalá-lo, veja [`REQUIREMENTS.md`](../../REQUIREMENTS.md)
+ [`INSTALL.md`](../../INSTALL.md).

## Em uma frase

O Xal-Tor-Ka é um **gatekeeper de autenticação + gerenciador de reverse proxy**: coloca o
**NGINX** como o único serviço exposto à internet e, para cada requisição, pergunta a um
**serviço Go interno** (nunca exposto) se deve deixar passar, pedir login ou negar.

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## Como ele decide: o fluxo `auth_request`

O NGINX **não conhece as regras**. Para cada requisição que chega, ele faz uma
*subrequest* interna ao endpoint `/validate` do serviço Go, passando o host e o
caminho originais. O serviço responde com um status HTTP:

- **200** → o NGINX prossegue com o `proxy_pass` para o backend real;
- **401** → o NGINX redireciona para a página de login;
- **403** → acesso negado.

O princípio é **fail-closed**: qualquer erro, timeout ou dúvida durante a avaliação
resulta em 401/403, **nunca** em 200. O serviço Go não é acessível de fora: somente
o NGINX, na rede interna, pode consultá-lo.

## O núcleo: a matriz de autorização

Para cada **host** e **caminho (path)**, aplica-se uma de três regras:

| Regra | Quem entra |
|------|-------------|
| `public` | qualquer pessoa |
| `authenticated` | usuários com sessão válida e **2FA** (TOTP) concluído |
| `whitelist` | apenas usuários **explicitamente autorizados** para aquele serviço |

**Administradores** podem acessar tudo. A granularidade é por subdomínio e por caminho
(por exemplo, `/` público mas `/admin` em whitelist no mesmo host).

## Autenticação

- **Local**: senhas com hash **argon2id** + um segundo fator via **TOTP**
  (RFC 6238, compatível com Google Authenticator/Authy).
- **OIDC** (OpenID Connect): login delegado ao **Google**, **Microsoft/Entra** ou
  provedores genéricos (**Keycloak, Authentik, Auth0, Okta, GitLab**). A assinatura
  do token de identidade é verificada contra as chaves públicas do provedor.
  - **Sem auto-provisionamento**: o usuário já deve existir, declarado para aquele
    provedor — entrar com o Google não basta para obter acesso. Veja
    [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md).
- **Sessões**: cookies `HttpOnly`/`SameSite=Lax` (e `Secure` atrás de HTTPS),
  mantidos em RAM com persistência em arquivo (sobrevivem a um reinício).

## Configuração: tudo em JSON

Nenhum banco de dados obrigatório: a configuração fica em alguns arquivos JSON tipados,
validados na inicialização (**Fail-Fast**: um campo desconhecido ou um valor fora do
intervalo bloqueia a inicialização com uma mensagem clara).

| Arquivo | Conteúdo |
|------|---------|
| `config.json` | infraestrutura (com templates de env): modo de auth, TLS, sessões, IPs de admin, provedores |
| `secrets.json` | segredos (client secrets OIDC, tokens, SMTP) — nunca versionado |
| `users.json` | usuários, papéis, 2FA, autorizações — nunca versionado |
| `services.json` | serviços gerenciados em runtime (backends proxiados + links de dashboard) |

Alterações em usuários/serviços são aplicadas **a quente** (hot reload), sem reinício.

## Componentes (serviço Go)

Stdlib-first, binário estático. Pacotes principais:

- `handlers/` — endpoints HTTP: `/validate`, login + TOTP, callback OIDC, setup,
  o painel `/admin`, o dashboard `/listing`.
- `providers/` — autenticação: `local` e `oidc` (interface comum).
- `matrix/` — avaliação das regras de autorização (por host/path).
- `proxy/` — gera os vhosts de backend do NGINX e recarrega.
- `health/` — verificações periódicas de saúde dos backends (endpoint `/health`).
- `config/` — carregamento + validação + salvamento atômico com snapshots.
- `audit/` — registro de tentativas de acesso falhas (para fail2ban).
- `auth/` — hashing, TOTP, sessões, diretório de usuários.

## Reverse proxy: geração e reload

O gerenciador gera a configuração de backend do NGINX (um `server{}` por host,
com `auth_request` nas rotas protegidas e `proxy_pass` para o upstream). O
**reload**:

- **Docker**: o container NGINX detecta a alteração e se recarrega sozinho (polling),
  porque o `inotify` é pouco confiável em bind mounts no Docker Desktop/WSL2.
- **Host/LXD**: o serviço Go executa um comando de reload configurável
  (`nginx -s reload` / `systemctl reload nginx`).

O NGINX sempre valida a nova configuração e, se ela for inválida, mantém a
que está em execução: uma regeneração ruim não derruba o proxy.

## Gerenciamento e operações

- **Painel `/admin`** (restrito por IP): gerencie serviços, usuários e permissões,
  monitore o status, em páginas separadas.
- **Dashboard `/listing`**: mostra a cada usuário apenas os serviços que ele pode acessar.
- **Onboarding**: a primeira execução gera um token com expiração para criar o primeiro
  administrador via web; depois a interface se blinda.
- **Backups**: cada salvamento cria um snapshot com auto-trash (mantém os últimos N),
  com restauração também via CLI.
- **Defesa contra força bruta**: tentativas falhas vão para um log estruturado
  (`logs/auth.log`) com o IP real do cliente, integrável ao **fail2ban**.

## Segurança em resumo

- O único serviço exposto é o **NGINX**; o serviço Go é interno.
- **Fail-closed** em todo o caminho de autorização.
- Comparações de segredos em **tempo constante**; segredos **nunca** registrados em log.
- Área de admin restrita por **IP**; o IP real do cliente é obtido de
  `X-Forwarded-For` apenas a partir de proxies confiáveis.
- O token de setup é de **uso único** e com expiração.

## Implantação (Deployment)

- **Docker Compose** (padrão): NGINX exposto, serviço Go interno, um sidecar
  somente-leitura para descoberta de containers, limites de recursos e rotação de logs.
- **Host / LXD / máquina dedicada**: binário estático + NGINX do sistema, governado por
  três variáveis (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`).
  Veja [`INSTALL.md`](../../INSTALL.md) §9.

## Versão

Fonte única de verdade em `version/version.go` (linha pré-1.0 `beta0.N`),
substituível em tempo de build e exibida em `xaltorka version`, `/healthz`, o log
de inicialização e a interface.
