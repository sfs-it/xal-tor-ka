// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package models defines the typed data structures of Xal-Tor-Ka:
// configuration, secrets, users and runtime entities. See BLUEPRINT.md §7–§8.
// No use of map[string]interface{} / any for known fields (MYRULES Go §5).
package models

import "time"

// Config is the non-secret application configuration (config.json, BLUEPRINT §7.1).
type Config struct {
	AuthMode     bool          `json:"auth_mode"`
	DisableTOTP  bool          `json:"disable_totp,omitempty"` // true = password only, no 2FA
	AuthLog      string        `json:"auth_log,omitempty"`     // failure log file (fail2ban)
	Server       ServerCfg     `json:"server"`
	TLS          TLSCfg        `json:"tls"`
	Session      SessionCfg    `json:"session"`
	Admin        AdminCfg      `json:"admin"`
	Providers    []ProviderCfg `json:"providers"`
	UsersFile    string        `json:"users_file"`
	SecretsFile  string        `json:"secrets_file"`
	ServicesFile string        `json:"services_file"`
	Monitoring   MonitoringCfg `json:"monitoring"`
	Backends     []Backend     `json:"backends"`
}

// ServerCfg holds the HTTP listen address and proxy trust settings.
type ServerCfg struct {
	Listen         string   `json:"listen"`
	ExternalURL    string   `json:"external_url"`
	TrustedProxies []string `json:"trusted_proxies"`
}

// TLSCfg selects the certificate provisioning strategy (BLUEPRINT §3.1).
type TLSCfg struct {
	Mode    string   `json:"mode"` // external|acme|selfsigned
	Domains []string `json:"domains"`
	ACME    ACMECfg  `json:"acme"`
}

// ACMECfg holds the non-secret ACME/PowerDNS settings (key lives in Secrets).
type ACMECfg struct {
	Provider   string `json:"provider"`
	PDNSAPIURL string `json:"pdns_api_url"`
	Email      string `json:"email"`
}

// SessionCfg holds session cookie and persistence settings.
type SessionCfg struct {
	CookieName         string `json:"cookie_name"`
	CookieDomain       string `json:"cookie_domain,omitempty"` // e.g. "localhost" → SSO on *.localhost
	TTLMinutes         int    `json:"ttl_minutes"`
	IdleTimeoutMinutes int    `json:"idle_timeout_minutes"`
	Store              string `json:"store"` // sqlite|memory
	SQLitePath         string `json:"sqlite_path"`
}

// AdminCfg restricts access to the administration interface.
type AdminCfg struct {
	IPWhitelist []string `json:"ip_whitelist"`
}

// ProviderCfg declares an authentication provider (secret lives in Secrets).
type ProviderCfg struct {
	ID       string `json:"id"`
	Type     string `json:"type"`           // oidc|local
	Name     string `json:"name,omitempty"` // login button label (default: id)
	Enabled  bool   `json:"enabled"`
	Issuer   string `json:"issuer,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

// MonitoringCfg groups the monitoring/alerting configuration.
type MonitoringCfg struct {
	Alerting AlertingCfg `json:"alerting"`
}

// AlertingCfg configures the alerting channels (BLUEPRINT §11).
type AlertingCfg struct {
	Telegram TelegramCfg `json:"telegram"`
	Email    EmailCfg    `json:"email"`
}

// TelegramCfg holds the non-secret Telegram settings (bot token in Secrets).
type TelegramCfg struct {
	Enabled bool   `json:"enabled"`
	ChatID  string `json:"chat_id"`
}

// EmailCfg holds the non-secret SMTP settings (credentials in Secrets).
type EmailCfg struct {
	Enabled  bool     `json:"enabled"`
	SMTPHost string   `json:"smtp_host"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

// Backend is a service behind the reverse proxy, with per-path rules. Name/URL
// are presentation hints for the /listing dashboard (URL is the public address
// the user clicks; if empty it is derived from Host).
type Backend struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Host        string `json:"host"`
	URL         string `json:"url,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"` // excluded from resolver/proxy/health
	// IPAllow is an optional per-vhost IP allow-list (CIDRs). When non-empty,
	// requests whose client IP is not covered are denied (403) before the rule is
	// evaluated — so it also restricts "public" services. Empty = no IP restriction.
	IPAllow []string `json:"ip_allow,omitempty"`
	Routes  []Route  `json:"routes"`
	Health  Health   `json:"health"`
}

// Link is an external service shown as a tile in /listing but NOT reverse
// proxied (a bookmark to an external URL). Visible if Public or the user is
// authorized for its ID (same authorization namespace as backend ids).
type Link struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Public      bool   `json:"public,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"` // hidden from the listing
}

// Services is the runtime-managed set of dashboard services (services.json),
// separate from config.json so it can be edited at runtime (admin/CLI) without
// touching the env-templated infrastructure config. Backends here are merged
// into the resolver; Links are dashboard-only tiles.
type Services struct {
	// AdminIPWhitelist, when non-empty, overrides config.admin.ip_whitelist at
	// runtime (editable from the admin UI, applied on hot reload). Empty = use the
	// config/env value (ADMIN_CIDR).
	AdminIPWhitelist []string  `json:"admin_ip_whitelist,omitempty"`
	Backends         []Backend `json:"backends"`
	Links            []Link    `json:"links"`
	Monitors         []Monitor `json:"monitors,omitempty"`
	// Providers are runtime-managed authentication providers (admin UI), merged
	// on top of config.json providers by id. Non-secret fields only; the OIDC
	// client_secret stays in secrets.json (keyed by provider id).
	Providers []ProviderCfg `json:"providers,omitempty"`
}

// Monitor is a standalone health probe shown in the admin Monitoring page,
// independent of the reverse-proxied backends (e.g. an external URL to watch).
type Monitor struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	URL             string `json:"url"`
	IntervalSeconds int    `json:"interval_seconds,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
}

// Route maps a path prefix to an access rule and an upstream address.
type Route struct {
	Path     string `json:"path"`
	Rule     string `json:"rule"` // public|authenticated|whitelist
	Upstream string `json:"upstream"`
}

// Health describes the HTTP health endpoint of a backend.
type Health struct {
	URL             string `json:"url"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

// Secrets holds external secrets (secrets.json, BLUEPRINT §7.2). Never logged.
type Secrets struct {
	AdminPasswordHash string                    `json:"admin_password_hash"`
	Providers         map[string]ProviderSecret `json:"providers"`
	Telegram          TelegramSecret            `json:"telegram"`
	SMTP              SMTPSecret                `json:"smtp"`
	ACME              ACMESecret                `json:"acme"`
}

// ProviderSecret is the OIDC client secret of a provider, keyed by provider id.
type ProviderSecret struct {
	ClientSecret string `json:"client_secret"`
}

// TelegramSecret holds the Telegram bot token.
type TelegramSecret struct {
	BotToken string `json:"bot_token"`
}

// SMTPSecret holds the SMTP credentials.
type SMTPSecret struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ACMESecret holds the PowerDNS API key for the ACME DNS-01 challenge.
type ACMESecret struct {
	PDNSAPIKey string `json:"pdns_api_key"`
}

// Users is the authorization matrix (users.json, BLUEPRINT §7.3). Contains
// per-user secrets (TOTP secret, local password hash): never logged.
type Users struct {
	Users []User `json:"users"`
}

// User maps an identity to its provider, secrets and authorized backends.
type User struct {
	Email        string   `json:"email"`
	Provider     string   `json:"provider"`
	PasswordHash string   `json:"password_hash,omitempty"`
	TOTPSecret   string   `json:"totp_secret"`
	Admin        bool     `json:"admin,omitempty"` // access to the /admin area
	Backends     []string `json:"backends"`
}

// Session is a runtime authenticated session (BLUEPRINT §8). Not persisted here.
type Session struct {
	ID        string
	Email     string
	Provider  string
	TwoFADone bool
	CreatedAt time.Time
	LastSeen  time.Time
}

// SetupToken is the single-use, expiring token for first-run setup (BLUEPRINT §13).
type SetupToken struct {
	Value     string
	ExpiresAt time.Time
	Used      bool
}

// SetupState is the on-disk state of the hybrid onboarding (data/setup.json):
// created by the `setup` CLI subcommand (token+email), then enriched by the web
// wizard (password hash + TOTP secret) across steps. Deleted on completion.
// Lives in the gitignored data/ dir with 0600 perms (transient secrets).
type SetupState struct {
	Token        string    `json:"token"`
	Email        string    `json:"email"`
	ExpiresAt    time.Time `json:"expires_at"`
	PasswordHash string    `json:"password_hash,omitempty"`
	TOTPSecret   string    `json:"totp_secret,omitempty"`
}
