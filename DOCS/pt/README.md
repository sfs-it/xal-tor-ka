# ⛬ Xal-Tor-Ka · `beta0.3`

> 🌍 **Idiomas:** [README oficial em inglês](../../README.md) ·
> [todos os idiomas](../README.md)

*Tradução do documento oficial em inglês; em caso de divergência, prevalece a versão em inglês.*

**Uma única entrada protegida diante de todos os seus serviços online.** O Xal-Tor-Ka é o
"porteiro digital" da sua infraestrutura: coloca uma única porta blindada diante de
cada site e aplicação que você publica e decide — para cada um — quem pode entrar.
Chega de senhas espalhadas serviço a serviço: a regra fica em um único lugar.

Para quem **toma decisões**, o valor é simples: você publica uma aplicação interna
sem expô-la ao mundo; você escolhe se um serviço é **aberto a
todos**, **reservado a usuários autenticados** (com verificação em duas etapas) ou
**apenas para pessoas explicitamente autorizadas**; e você gerencia tudo a partir de um
**painel web protegido**. Os usuários podem entrar com credenciais internas ou com a
conta corporativa **Google** ou **Microsoft**. O sistema mantém um registro de
acessos, defende-se contra tentativas de força bruta e faz backup da própria configuração.

> 👉 Se você quer apenas saber **o que ele faz e como faz**, vá para
> **[`TECHNOTES.md`](./TECHNOTES.md)** (técnico, mas legível).
> Se você precisa **instalá-lo ou verificar os pré-requisitos**, consulte
> **[`REQUIREMENTS.md`](../../REQUIREMENTS.md)** e **[`INSTALL.md`](../../INSTALL.md)**.

## Para quem se destina

- Você opera vários serviços web (intranet, aplicações de negócio, dashboards,
  ferramentas internas) e quer **um único ponto de entrada controlado** em vez de
  proteger cada um separadamente.
- Você quer **decidir de forma centralizada** quem vê o quê, com um segundo fator
  de segurança e/ou login corporativo (Google/Microsoft), **sem modificar os
  serviços individuais**.
- Você quer que a única coisa exposta à internet seja um **escudo**, enquanto os
  serviços reais permanecem ocultos por trás dele.

## O que você obtém

- **Uma única porta blindada**: o restante da infraestrutura não é acessível
  diretamente pela internet.
- **Três níveis de acesso** por serviço: público · apenas usuários autenticados (2FA) ·
  apenas pessoas na lista de permissões.
- **Login com Google / Microsoft** ou com credenciais internas.
- **Um painel web protegido** para gerenciar serviços, usuários e permissões.
- **Segurança operacional**: registro de acessos (integrável a sistemas de
  prevenção de intrusões), backups automáticos da configuração, monitoramento da
  saúde dos serviços.

## Status

**`beta0.3`** — uma versão pré-1.0. O núcleo está construído e funcionando; alguns
recursos avançados ainda estão sendo refinados (veja [`TODO.md`](../../TODO.md)).

## Documentação

| Documento | Público | Conteúdo |
|----------|----------|---------|
| **[TECHNOTES.md](./TECHNOTES.md)** | avaliadores curiosos / técnicos | o que ele faz e **como** |
| **[REQUIREMENTS.md](../../REQUIREMENTS.md)** | engenheiros | o que você precisa para executá-lo |
| **[INSTALL.md](../../INSTALL.md)** | engenheiros | instalação passo a passo (Docker e não-Docker) |
| **[AUTH-PROVIDERS.md](../../AUTH-PROVIDERS.md)** | engenheiros | habilitar Google / Microsoft / OIDC |
| **[BLUEPRINT.md](../../BLUEPRINT.md)** | desenvolvedores | especificação de arquitetura autoritativa |

As traduções dos documentos de entrada (README, TECHNOTES) ficam em
[`DOCS/`](../README.md). As **versões em inglês na raiz do repositório são
oficiais**; em caso de qualquer divergência, prevalece o inglês.

## Nome

**`Xal-Tor-Ka`** quando você fala *sobre* ele (marca, interface, documentação); **`xaltorka`** quando você
fala *com* ele (módulo Go, binário, serviço Docker, hostname).

---

© 2026 **SFS.it di Zanutto Agostino** — distribuído sob a
[Apache License 2.0](../../LICENSE).
