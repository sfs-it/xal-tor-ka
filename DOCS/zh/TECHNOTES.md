# Xal-Tor-Ka —— 技术说明（它做什么以及怎么做）

*本文为官方英文文档的译文；如有出入，以英文版本为准。*

一份技术性但易读的工作原理说明。如需**严谨的规范**（JSON 模式、数据模型、
端点契约），请参阅 [`BLUEPRINT.md`](../../BLUEPRINT.md)；如需安装，请参阅
[`REQUIREMENTS.md`](../../REQUIREMENTS.md) + [`INSTALL.md`](../../INSTALL.md)。

## 一句话概括

Xal-Tor-Ka 是一个**认证守门人 + 反向代理管理器**：它将 **NGINX** 作为唯一
暴露在互联网上的服务，并对每个请求询问一个**内部 Go 服务**（从不暴露）：
应当放行、要求登录，还是拒绝。

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## 它如何决策：`auth_request` 流程

NGINX **并不知道规则**。对于每个进入的请求，它会向 Go 服务的 `/validate`
端点发起一个内部*子请求*（subrequest），传递原始的主机和路径。该服务以一个
HTTP 状态码回复：

- **200** → NGINX 继续以 `proxy_pass` 转发到真实后端；
- **401** → NGINX 重定向到登录页面；
- **403** → 访问被拒绝。

其原则是 **fail-closed**（失败即关闭）：评估过程中的任何错误、超时或疑问都会
得出 401/403，**绝不**返回 200。Go 服务无法从外部访问：只有内部网络上的
NGINX 才能查询它。

## 核心：授权矩阵

对于每个**主机**和**路径**，适用以下三种规则之一：

| 规则 | 谁可以进入 |
|------|-------------|
| `public` | 任何人 |
| `authenticated` | 拥有有效会话并已完成 **2FA**（TOTP）的用户 |
| `whitelist` | 仅限对该服务**明确授权**的用户 |

**管理员**可以访问一切。粒度可细化到每个子域名和每个路径
（例如同一主机上 `/` 为公开，而 `/admin` 为白名单）。

## 认证

- **本地（Local）**：密码使用 **argon2id** 哈希，第二因素通过 **TOTP**
  （RFC 6238，兼容 Google Authenticator/Authy）。
- **OIDC**（OpenID Connect）：登录委托给 **Google**、**Microsoft/Entra** 或
  通用提供商（**Keycloak、Authentik、Auth0、Okta、GitLab**）。身份令牌的签名
  会针对提供商的公钥进行验证。
  - **无自动开通（no auto-provisioning）**：用户必须已存在，且已为该提供商声明
    —— 仅用 Google 登录不足以进入。参见
    [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md)。
- **会话（Sessions）**：`HttpOnly`/`SameSite=Lax` cookie（在 HTTPS 之后还会加上
  `Secure`），保存在内存中并带文件持久化（重启后仍然有效）。

## 配置：一切皆 JSON

无强制数据库：配置存放于若干个带类型的 JSON 文件中，在启动时进行验证
（**Fail-Fast**：未知字段或超出范围的值会阻止启动，并给出清晰的提示）。

| 文件 | 内容 |
|------|---------|
| `config.json` | 基础设施（支持环境变量模板化）：认证模式、TLS、会话、管理员 IP、提供商 |
| `secrets.json` | 机密（OIDC client secret、令牌、SMTP）—— 绝不纳入版本控制 |
| `users.json` | 用户、角色、2FA、授权 —— 绝不纳入版本控制 |
| `services.json` | 运行时管理的服务（被代理的后端 + 仪表板链接） |

对用户/服务的更改会**热生效**（热重载），无需重启。

## 组件（Go 服务）

以标准库优先（stdlib-first），静态二进制文件。主要包：

- `handlers/` —— HTTP 端点：`/validate`、登录 + TOTP、OIDC 回调、setup、
  `/admin` 面板、`/listing` 仪表板。
- `providers/` —— 认证：`local` 和 `oidc`（共用接口）。
- `matrix/` —— 授权规则的评估（按主机/路径）。
- `proxy/` —— 生成 NGINX 后端 vhost 并重载。
- `health/` —— 周期性后端健康检查（`/health` 端点）。
- `config/` —— 加载 + 验证 + 带快照的原子保存。
- `audit/` —— 失败访问尝试的日志（供 fail2ban 使用）。
- `auth/` —— 哈希、TOTP、会话、用户目录。

## 反向代理：生成与重载

管理器会生成 NGINX 后端配置（每个主机一个 `server{}`，在受保护路由上带
`auth_request`，并以 `proxy_pass` 指向上游）。**重载（reload）**：

- **Docker**：NGINX 容器检测到更改后自行重载（轮询方式），因为在
  Docker Desktop/WSL2 的 bind mount 上 `inotify` 并不可靠。
- **Host/LXD**：Go 服务执行一条可配置的重载命令
  （`nginx -s reload` / `systemctl reload nginx`）。

NGINX 始终会验证新配置，若其无效，则保留正在运行的配置：一次糟糕的重新生成
不会让代理宕机。

## 管理与运维

- **`/admin` 面板**（限制 IP）：在各自独立的页面中管理服务、用户和权限，
  并监控状态。
- **`/listing` 仪表板**：仅向每个用户展示其可访问的服务。
- **初始化引导（Onboarding）**：首次运行会生成一个有时效的令牌，用于通过
  Web 创建第一个管理员；随后界面即锁定。
- **备份（Backups）**：每次保存都会创建一个带自动清理（auto-trash，保留最近 N 个）
  的快照，也支持从 CLI 恢复。
- **暴力破解防御**：失败的尝试会进入结构化日志（`logs/auth.log`），其中包含
  真实客户端 IP，可接入 **fail2ban**。

## 安全速览

- 唯一暴露的服务是 **NGINX**；Go 服务为内部服务。
- 在整个授权路径上贯彻 **fail-closed**。
- 机密比较采用**常数时间**（constant time）；机密**绝不**写入日志。
- 管理区域按 **IP** 限制；真实客户端 IP 仅从受信代理的 `X-Forwarded-For` 中获取。
- setup 令牌为**一次性**且有时效。

## 部署

- **Docker Compose**（默认）：NGINX 暴露，Go 服务为内部服务，外加一个用于容器
  发现的只读 sidecar，并配有资源限制与日志轮转。
- **Host / LXD / 专用机器**：静态二进制文件 + 系统 NGINX，由三个变量
  （`DEPLOY_MODE`、`NGINX_RELOAD_CMD`、`UPSTREAM_LOCALHOST`）管控。
  参见 [`INSTALL.md`](../../INSTALL.md) §9。

## 版本

唯一真实来源位于 `version/version.go`（1.0 之前的 `beta0.N` 序列），可在
构建时覆盖，并显示在 `xaltorka version`、`/healthz`、启动日志和 UI 中。
