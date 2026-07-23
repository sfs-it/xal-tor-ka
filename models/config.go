// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package models defines the typed data structures of Xal-Tor-Ka:
// configuration, secrets, users and runtime entities. See BLUEPRINT.md §7–§8.
// No use of map[string]interface{} / any for known fields (MYRULES Go §5).
package models

import "time"

// Config is the non-secret application configuration (config.json, BLUEPRINT §7.1).
type Config struct {
	AuthMode    bool   `json:"auth_mode"`
	DisableTOTP bool   `json:"disable_totp,omitempty"` // true = password only, no 2FA
	AuthLog     string `json:"auth_log,omitempty"`     // failure log file (fail2ban)
	// OneTimeCode (optional) enables passwordless login via a one-time code delivered
	// out-of-band. Disabled by default; the code is a first factor (TOTP still applies).
	OneTimeCode  OneTimeCodeCfg `json:"one_time_code,omitempty"`
	Server       ServerCfg      `json:"server"`
	TLS          TLSCfg         `json:"tls"`
	Session      SessionCfg     `json:"session"`
	Admin        AdminCfg       `json:"admin"`
	Providers    []ProviderCfg  `json:"providers"`
	UsersFile    string         `json:"users_file"`
	SecretsFile  string         `json:"secrets_file"`
	ServicesFile string         `json:"services_file"`
	Monitoring   MonitoringCfg  `json:"monitoring"`
	// RemoteControl (optional) enables receiving vetted commands + sending notifications
	// over Telegram/email. Disabled by default; fail-closed.
	RemoteControl RemoteControlCfg `json:"remote_control,omitempty"`
	// OSUpdates (optional) enables checking/notifying (and optionally auto-applying)
	// host OS package updates via the vetted agent. Disabled by default.
	OSUpdates OSUpdatesCfg `json:"os_updates,omitempty"`
	Backends  []Backend    `json:"backends"`
}

// OSUpdatesCfg configures host OS package-update checking, notification, and optional
// automatic application (via the vetted agent's os_updates_* commands). Fail-safe:
// the check is read-only; applying never reboots on its own.
type OSUpdatesCfg struct {
	// Automation level. "" or "off" = disabled; "notify" = check + notify only (no
	// apply); "security" = auto-apply security updates; "all" = auto-apply everything.
	Automation string `json:"automation,omitempty"`
	// Notify: send a notification when updates are found.
	Notify bool `json:"notify,omitempty"`
	// NotifyOn: "any" (any available update) or "security" (only when security updates exist).
	NotifyOn string `json:"notify_on,omitempty"`
	// Channels selects notification channels ("telegram", "email"); empty = all configured.
	Channels []string `json:"channels,omitempty"`
	// PollHours is the check interval in hours (default 24).
	PollHours int `json:"poll_hours,omitempty"`
}

// OneTimeCodeCfg configures passwordless login via a one-time code. Disabled by default.
// The code is delivered out-of-band by the selected Channel; "spool" (default when enabled,
// e.g. no SMTP yet) writes it to a queue file for manual/audited retrieval, "email" uses the
// notify transport, "sms" is reserved for a later API integration. The code is a FIRST factor:
// a user with TOTP still completes 2FA after it.
type OneTimeCodeCfg struct {
	Enabled bool `json:"enabled,omitempty"`
	// Channel: "spool" (default) | "email" | "sms".
	Channel string `json:"channel,omitempty"`
	// TTLMinutes is the code validity window (default 10).
	TTLMinutes int `json:"ttl_minutes,omitempty"`
	// CodeLength is the number of decimal digits (default 6, min 4).
	CodeLength int `json:"code_length,omitempty"`
	// CooldownSeconds is the minimum gap between two requests for the same email (default 30).
	CooldownSeconds int `json:"cooldown_seconds,omitempty"`
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
	Type     string `json:"type"`           // oidc|local|ldap
	Name     string `json:"name,omitempty"` // login button label (default: id)
	Enabled  bool   `json:"enabled"`
	Issuer   string `json:"issuer,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	// LDAP (type "ldap"): bind-based auth against a directory / Active Directory.
	// See docs/next-gen-auth-sources.md. No secret required for direct-bind.
	LDAPURL                string `json:"ldap_url,omitempty"`                  // ldaps://host:636 or ldap://host:389
	LDAPBindDNTemplate     string `json:"ldap_bind_dn_template,omitempty"`     // %s = username, e.g. "%s@corp.example.com"
	LDAPBaseDN             string `json:"ldap_base_dn,omitempty"`              // optional (future: search / group mapping)
	LDAPStartTLS           bool   `json:"ldap_start_tls,omitempty"`            // upgrade a plain :389 connection to TLS
	LDAPInsecureSkipVerify bool   `json:"ldap_insecure_skip_verify,omitempty"` // skip cert verification — labs only
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

// RemoteControlCfg configures inbound remote control (receive vetted commands + send
// notifications) over Telegram and email. Disabled by default; every channel is
// fail-closed — only allow-listed senders, only a vetted command allow-list.
type RemoteControlCfg struct {
	Enabled       bool          `json:"enabled"`
	AllowCommands []string      `json:"allow_commands,omitempty"` // vetted command names; empty = built-in read-only set
	Telegram      TelegramInCfg `json:"telegram"`
	Email         EmailInCfg    `json:"email"`
}

// TelegramInCfg receives commands via the Telegram bot (token reused from Secrets).
// Only the listed chat IDs may issue commands.
type TelegramInCfg struct {
	Enabled      bool     `json:"enabled"`
	AllowChatIDs []string `json:"allow_chat_ids,omitempty"`
	PollSeconds  int      `json:"poll_seconds,omitempty"` // getUpdates interval (default 5s)
}

// EmailInCfg receives commands from a dedicated IMAP spool folder. Messages must be
// DKIM-valid and from an allow-listed sender/domain (password in Secrets.IMAP).
type EmailInCfg struct {
	Enabled     bool     `json:"enabled"`
	IMAPHost    string   `json:"imap_host,omitempty"`
	IMAPPort    int      `json:"imap_port,omitempty"`
	User        string   `json:"user,omitempty"`
	Folder      string   `json:"folder,omitempty"`       // spool folder; empty = INBOX
	RequireDKIM bool     `json:"require_dkim,omitempty"` // verify the DKIM signature (recommended)
	AllowFrom   []string `json:"allow_from,omitempty"`   // allowed senders/domains (DKIM-verified)
	PollSeconds int      `json:"poll_seconds,omitempty"` // fetch interval (default 30s)
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
	WWW         bool   `json:"www,omitempty"`      // also serve/cert www.<host> (extra server_name + cert SAN)
	Disabled    bool   `json:"disabled,omitempty"` // excluded from resolver/proxy/health
	// Unlisted hides this service from the public /listing (default: shown). Description
	// (Markdown, sanitized) and Image (preview) enrich its card on that page.
	Unlisted bool   `json:"unlisted,omitempty"`
	Image    string `json:"image,omitempty"` // preview image filename under data/listing-img/, shown on the listing card
	// IPAllow is an optional per-vhost IP allow-list (CIDRs). When non-empty,
	// requests whose client IP is not covered are denied (403) before the rule is
	// evaluated — so it also restricts "public" services. Empty = no IP restriction.
	IPAllow []string  `json:"ip_allow,omitempty"`
	Routes  []Route   `json:"routes"`
	Health  Health    `json:"health"`
	Nginx   NginxOpts `json:"nginx,omitempty"` // per-vhost NGINX tuning (optional)
	// Hosting, when set, marks this backend as owned by the hosting extension: its
	// upstream is fixed (<site[-vhost]>.site:8080) and belongs to the Hosting panel.
	// The Services editor locks the upstream and links back to Hosting.
	Hosting *HostingRef `json:"hosting,omitempty"`
	// Waf, when enabled, puts ModSecurity + OWASP CRS in front of this service.
	Waf *WafCfg `json:"waf,omitempty"`
}

// WafCfg is the optional per-backend Web Application Firewall (ModSecurity v3 +
// OWASP Core Rule Set). Mode "detect" logs only; "block" returns 403 on a hit.
// Paranoia/Threshold are stored for future per-vhost tuning (v1 uses CRS defaults).
type WafCfg struct {
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode,omitempty"`      // "detect" (default) | "block"
	Paranoia  int    `json:"paranoia,omitempty"`  // CRS paranoia level 1..4 (0 = default)
	Threshold int    `json:"threshold,omitempty"` // inbound anomaly threshold (0 = default)
	// DisabledRules are CRS rule IDs to remove for THIS vhost (false-positive relief),
	// e.g. 942100. IgnoreIPs are client IPs/CIDRs that bypass the WAF entirely for this
	// vhost (the engine is turned off for their requests).
	DisabledRules []int    `json:"disabled_rules,omitempty"`
	IgnoreIPs     []string `json:"ignore_ips,omitempty"`
	// CustomRules is raw ModSecurity directives injected verbatim into this vhost's
	// rules (advanced) — typically conditional disable rules, e.g.
	// `SecRule REQUEST_URI "@beginsWith /api/upload" "id:9009500,phase:1,pass,nolog,ctl:ruleEngine=Off"`.
	// Loaded before the CRS so phase-1 conditions win. Validated by `nginx -t` on reload.
	CustomRules string `json:"custom_rules,omitempty"`
}

// HostingRef ties a backend to the hosting site/vhost that owns it.
type HostingRef struct {
	Site  string `json:"site"`
	Vhost string `json:"vhost"`
}

// NginxOpts are optional per-vhost NGINX knobs surfaced in the admin "NGINX
// settings" section. Zero values keep the current default behaviour. The custom
// blocks are raw directives for cases not covered by the toggles; NGINX validates
// them on reload (nginx -t) and the poller keeps the old config if they are wrong.
type NginxOpts struct {
	// ProxyTimeout sets proxy_read_timeout/proxy_send_timeout in seconds (0 = default 60).
	ProxyTimeout int `json:"proxy_timeout,omitempty"`
	// MaxBodyMB sets client_max_body_size in megabytes (0 = NGINX default 1m).
	MaxBodyMB int `json:"max_body_mb,omitempty"`
	// WebSocket adds the HTTP/1.1 Upgrade/Connection headers for WebSocket backends.
	WebSocket bool `json:"websocket,omitempty"`
	// NoBuffering disables proxy_buffering (streaming / Server-Sent Events).
	NoBuffering bool `json:"no_buffering,omitempty"`
	// BackendSelfSigned skips upstream TLS verification (proxy_ssl_verify off) and
	// enables SNI (proxy_ssl_server_name on) for HTTPS backends with a private cert.
	BackendSelfSigned bool `json:"backend_self_signed,omitempty"`
	// CustomServer / CustomLocation are raw directives injected verbatim in the
	// server{} / each proxied location{} block.
	CustomServer   string `json:"custom_server,omitempty"`
	CustomLocation string `json:"custom_location,omitempty"`
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
	Rule     string `json:"rule"` // public|authenticated|authorized
	Upstream string `json:"upstream"`
}

// The access rules, from the most open to the most closed. The distinction that
// matters is authentication vs authorisation: RuleAuthenticated only asks WHO you
// are (any account on the gate gets in), RuleAuthorized also asks whether you were
// granted this specific service.
const (
	RulePublic        = "public"        // no login at all
	RuleAuthenticated = "authenticated" // any user with a valid session
	RuleAuthorized    = "authorized"    // only users explicitly enabled on this service
	// ruleWhitelistLegacy is the former name of RuleAuthorized. It was replaced
	// because this product ALSO has a per-service IP allow-list, so "whitelist" read
	// as two different things on the same screen. Files written before the rename are
	// still accepted and are canonicalised on load.
	ruleWhitelistLegacy = "whitelist"
)

// CanonicalRule maps a stored rule to its current name, so configuration written
// before the rename keeps working unchanged.
func CanonicalRule(r string) string {
	if r == ruleWhitelistLegacy {
		return RuleAuthorized
	}
	return r
}

// CanonicalizeRules rewrites the rules of every route in place. Call it right after
// loading, so the rest of the code only ever sees the current vocabulary.
func CanonicalizeRules(bs []Backend) {
	for i := range bs {
		for j := range bs[i].Routes {
			bs[i].Routes[j].Rule = CanonicalRule(bs[i].Routes[j].Rule)
		}
	}
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
	IMAP              IMAPSecret                `json:"imap"`
}

// IMAPSecret holds the IMAP password for the remote-control email channel.
type IMAPSecret struct {
	Password string `json:"password"`
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
