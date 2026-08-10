// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Command xaltorka is the authentication gatekeeper + reverse-proxy manager.
// It loads the three-file configuration and serves the auth_request validation
// endpoint, the local login + TOTP flow and the first-run setup wizard.
//
// Subcommands:
//
//	xaltorka hashpw [password]      print an argon2id PHC hash (stdin if omitted)
//	xaltorka setup  --email <addr>  create a one-time setup profile + token URL
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"xaltorka/audit"
	"xaltorka/auth"
	"xaltorka/certmgr"
	"xaltorka/config"
	"xaltorka/handlers"
	"xaltorka/health"
	"xaltorka/matrix"
	"xaltorka/models"
	"xaltorka/notify"
	"xaltorka/providers"
	"xaltorka/proxy"
	"xaltorka/remote"
	"xaltorka/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println("xaltorka", version.Version)
			return
		case "hashpw":
			exitOnErr(runHashPW(os.Args[2:]))
			return
		case "setup":
			exitOnErr(runSetup(os.Args[2:]))
			return
		case "add-link":
			exitOnErr(runAddLink(os.Args[2:]))
			return
		case "add-backend":
			exitOnErr(runAddBackend(os.Args[2:]))
			return
		case "user":
			exitOnErr(runUser(os.Args[2:]))
			return
		case "backups":
			exitOnErr(runBackups(os.Args[2:]))
			return
		case "restore":
			exitOnErr(runRestore(os.Args[2:]))
			return
		case "cert":
			exitOnErr(runCert(os.Args[2:]))
			return
		case "admin-cidr":
			exitOnErr(runAdminCIDR(os.Args[2:]))
			return
		}
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configDir := flag.String("config", ".", "directory with config.json, secrets.json, users.json")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("xal-tor-ka", "version", version.Version)

	bundle, err := config.Load(*configDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dir := auth.NewUserDirectory(bundle.Users.Users)

	auditLog, err := audit.New(resolvePath(*configDir, bundle.Config.AuthLog, "logs/auth.log"))
	if err != nil {
		slog.Warn("audit log not available", "err", err)
	}

	ttl := time.Duration(bundle.Config.Session.TTLMinutes) * time.Minute
	idle := time.Duration(bundle.Config.Session.IdleTimeoutMinutes) * time.Minute
	var store auth.SessionStore
	if bundle.Config.Session.Store == "memory" {
		store = auth.NewMemoryStore(ttl, idle)
	} else {
		// "file" (and "sqlite", not yet native) → persistence to a JSON file.
		if bundle.Config.Session.Store == "sqlite" {
			slog.Warn("session store 'sqlite' not yet native: using file persistence")
		}
		sessPath := resolvePath(*configDir, bundle.Config.Session.SQLitePath, "data/sessions.json")
		store = auth.NewPersistentStore(ttl, idle, sessPath)
	}

	// Deployment knobs (backwards-compatible: the defaults apply to Docker).
	// DEPLOY_MODE=docker|host selects the profile; the two specific env vars override it.
	deployMode := getenv("DEPLOY_MODE", "docker")
	defReload, defUpstreamLocal := "", "host.docker.internal"
	if deployMode == "host" {
		defReload, defUpstreamLocal = "nginx -s reload", "127.0.0.1"
	}
	reloadCmd := getenv("NGINX_RELOAD_CMD", defReload)
	upstreamLocal := getenv("UPSTREAM_LOCALHOST", defUpstreamLocal)
	slog.Info("deploy mode", "mode", deployMode, "nginx_reload", reloadCmd != "", "upstream_localhost", upstreamLocal)

	srvHandlers := &handlers.Server{
		Cfg:               &bundle.Config,
		Users:             dir,
		Sessions:          store,
		Resolver:          matrix.NewResolver(&bundle.Config),
		Local:             providers.NewLocal(dir),
		LDAP:              handlers.BuildLDAP(bundle.Config.Providers),
		OIDC:              handlers.BuildOIDC(bundle.Config.Providers, bundle.Secrets, bundle.Config.Server.ExternalURL),
		UpstreamLocalhost: upstreamLocal,
		UsersPath:         resolvePath(*configDir, bundle.Config.UsersFile, "users.json"),
		BackupsDir:        filepath.Join(*configDir, "backups"),
		SetupPath:         filepath.Join(*configDir, "data", "setup.json"),
		ServicesPath:      resolvePath(*configDir, bundle.Config.ServicesFile, "services.json"),
		SecretsPath:       resolvePath(*configDir, bundle.Config.SecretsFile, "secrets.json"),
		BaseBackends:      bundle.Config.Backends,
		BaseProviders:     bundle.Config.Providers,
		DockerProxyURL:    getenv("DOCKER_PROXY", ""),
		DockerExclude:     splitCSV(getenv("DISCOVER_EXCLUDE", "xaltorka,docker-socket-proxy")),
		HostingUpstream:   getenv("HOSTING_UPSTREAM", ""), // set → enables /admin/hosting proxy + nav
		Audit:             auditLog,
		Proxy: &proxy.Manager{
			OutPath:    filepath.Join(*configDir, "nginx", "conf.d", "backends.conf"),
			BackupsDir: filepath.Join(*configDir, "backups"),
			ReloadCmd:  reloadCmd,
			Gen: proxy.GenConfig{
				Upstream:     getenv("PROXY_UPSTREAM", "xaltorka:8080"),
				GateLoginURL: strings.TrimRight(bundle.Config.Server.ExternalURL, "/"),
				Resolver:     getenv("PROXY_RESOLVER", "127.0.0.11"),
			},
		},
	}
	// TLS certificate manager: internal CA / self-signed + ACME HTTP-01. Certs are
	// written to <configdir>/certs (shared with the NGINX container) and referenced
	// in the generated vhosts. Wire cert-awareness into the proxy generator BEFORE
	// the first Reload so HTTPS listeners are emitted from the start.
	certMgr := &certmgr.Manager{
		Dir:          filepath.Join(*configDir, "certs"),
		NginxDir:     getenv("NGINX_CERT_DIR", "/etc/nginx/certs"),
		Email:        bundle.Config.TLS.ACME.Email,
		DirectoryURL: getenv("ACME_DIRECTORY_URL", ""),
	}
	certMgr.Reload = func() error { return srvHandlers.Reload() }
	srvHandlers.CertMgr = certMgr
	srvHandlers.Proxy.Gen.CertDir = certMgr.NginxDir
	srvHandlers.Proxy.Gen.HasCert = certMgr.HasCert

	// One-time-code (passwordless) login: opt-in via config. Codes live in memory;
	// the spool channel writes them (with requester IP) to data/otp-queue.jsonl.
	if bundle.Config.OneTimeCode.Enabled {
		ttlMin := bundle.Config.OneTimeCode.TTLMinutes
		if ttlMin <= 0 {
			ttlMin = 10
		}
		cd := bundle.Config.OneTimeCode.CooldownSeconds
		if cd <= 0 {
			cd = 30
		}
		srvHandlers.OTP = auth.NewOTPStore(time.Duration(ttlMin)*time.Minute, time.Duration(cd)*time.Second, bundle.Config.OneTimeCode.CodeLength)
		srvHandlers.OTPQueuePath = filepath.Join(*configDir, "data", "otp-queue.jsonl")
	}

	// Merge services.json (extra backends + link tiles) into the resolver.
	if err := srvHandlers.Reload(); err != nil {
		slog.Error("services reload failed", "err", err)
	}

	// Boot-time LOUD check. The core regenerates nginx/conf.d/backends.conf with an
	// atomic write, which needs permission on the DIRECTORY. When a deploy leaves
	// that directory root-owned the degradation is SILENT: services.json still
	// updates and the admin UI looks healthy, but no publish and no certificate
	// ever reaches NGINX. A WARN was not enough — it hid the fault for a full day
	// in production. Shout in the log AND fire the alerting channel.
	if err := srvHandlers.Proxy.CheckWritable(); err != nil {
		confDir := filepath.Join(*configDir, "nginx", "conf.d")
		slog.Error("CRITICAL: cannot regenerate the NGINX config — publish and TLS changes will NOT reach NGINX",
			"err", err, "fix", "chown 1000:1000 "+confDir)
		notify.New(bundle.Config.Monitoring.Alerting, bundle.Secrets).Send(
			"Xal-Tor-Ka "+version.Version+" — NGINX config NOT writable",
			"The gateway cannot regenerate "+filepath.Join(confDir, "backends.conf")+".\n"+
				"Publish and TLS changes will NOT reach NGINX (the admin UI will still look fine).\n\n"+
				"Error: "+err.Error()+"\n"+
				"Fix: chown 1000:1000 "+confDir+"  then restart the gateway.")
	}

	// Health checker: probes backend /health endpoints, feeds the admin Monitoring.
	checker := health.New(srvHandlers.HealthTargets,
		health.NewAlerter(bundle.Config.Monitoring.Alerting, bundle.Secrets))
	srvHandlers.Health = checker

	slog.Info("config loaded",
		"auth_mode", bundle.Config.AuthMode,
		"tls_mode", bundle.Config.TLS.Mode,
		"providers", len(bundle.Config.Providers),
		"backends", len(bundle.Config.Backends),
		"services", len(bundle.Services.Backends),
		"links", len(bundle.Services.Links),
		"users", dir.Count(),
	)

	srv := &http.Server{
		Addr:              bundle.Config.Server.Listen,
		Handler:           srvHandlers.Routes(),
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// SIGHUP hot-reloads services.json (admin IP whitelist, backends, providers)
	// with no restart — the lockout-proof apply path for the `admin-cidr` CLI:
	// no HTTP, no admin session, no IP gate. Safe to send locally / in-container
	// to PID 1. It runs the same Reload() as POST /admin/reload.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := srvHandlers.Reload(); err != nil {
				slog.Error("SIGHUP reload failed", "err", err)
			} else {
				slog.Info("reloaded via SIGHUP")
			}
		}
	}()

	go checker.Start(ctx)

	// Remote control (optional): an inbound channel (Telegram now, IMAP+DKIM later) that
	// runs VETTED read-only commands, plus a startup notification. Fail-closed and
	// disabled unless configured.
	if bundle.Config.RemoteControl.Enabled {
		notifier := notify.New(bundle.Config.Monitoring.Alerting, bundle.Secrets)
		commands := map[string]func(string) string{
			"ping":     func(string) string { return "pong" },
			"version":  func(string) string { return "Xal-Tor-Ka " + version.Version },
			"status":   func(string) string { return remoteStatus(checker, srvHandlers.Resolver.Backends()) },
			"backends": func(string) string { return remoteBackends(checker) },
		}
		remote.New(bundle.Config.RemoteControl, bundle.Secrets.Telegram.BotToken, commands, slog.Default()).Start(ctx)
		notifier.Send("Xal-Tor-Ka", "gateway started · "+version.Version)
	}

	// Renew ACME certs approaching expiry (self-signed ones are left as-is).
	go certMgr.StartRenewal(ctx, func() []string {
		var hs []string
		for _, b := range srvHandlers.Resolver.Backends() {
			if b.Host != "" {
				hs = append(hs, b.Host)
			}
		}
		return hs
	}, 30*24*time.Hour)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	slog.Info("server stopped")
	return nil
}

// remoteStatus is the read-only "status" remote command: version + up/down backend count.
func remoteStatus(c *health.Checker, backends []models.Backend) string {
	up, down := 0, 0
	for _, s := range c.Snapshot() {
		switch s.State {
		case health.StateUp:
			up++
		case health.StateDown, health.StateUnreachable:
			down++
		}
	}
	return fmt.Sprintf("Xal-Tor-Ka %s\nbackends: %d configured · %d up · %d down",
		version.Version, len(backends), up, down)
}

// remoteBackends is the read-only "backends" remote command: per-backend health.
func remoteBackends(c *health.Checker) string {
	snap := c.Snapshot()
	if len(snap) == 0 {
		return "no monitored backends"
	}
	var b strings.Builder
	for _, s := range snap {
		fmt.Fprintf(&b, "%s (%s): %s\n", s.BackendID, s.Host, s.State)
	}
	return strings.TrimRight(b.String(), "\n")
}

// runCert issues, renews or lists TLS certificates from the CLI: ACME/Let's
// Encrypt via the webroot HTTP-01 challenge, or the internal self-signed CA.
// It needs the stack running (NGINX serves the webroot + the vhost); it writes
// the certificate and regenerates the NGINX config so the HTTPS vhost appears
// (the NGINX poller reloads). No admin session or HTTPS bootstrap needed.
//
//	xaltorka cert issue <host> [--self-signed]
//	xaltorka cert renew <host>
//	xaltorka cert list
func runCert(args []string) error {
	fs := flag.NewFlagSet("cert", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	selfSigned := fs.Bool("self-signed", false, "issue via the internal CA instead of ACME")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 1 {
		return errors.New("usage: cert <issue|renew|list> [host] [--self-signed]")
	}
	bundle, err := config.Load(*dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defReload := ""
	if getenv("DEPLOY_MODE", "docker") == "host" {
		defReload = "nginx -s reload"
	}
	nginxCertDir := getenv("NGINX_CERT_DIR", "/etc/nginx/certs")
	pm := &proxy.Manager{
		OutPath:    filepath.Join(*dir, "nginx", "conf.d", "backends.conf"),
		BackupsDir: filepath.Join(*dir, "backups"),
		ReloadCmd:  getenv("NGINX_RELOAD_CMD", defReload),
		Gen: proxy.GenConfig{
			Upstream: getenv("PROXY_UPSTREAM", "xaltorka:8080"),
			Resolver: getenv("PROXY_RESOLVER", "127.0.0.11"),
			CertDir:  nginxCertDir,
		},
	}
	cm := &certmgr.Manager{
		Dir:          filepath.Join(*dir, "certs"),
		NginxDir:     nginxCertDir,
		Email:        bundle.Config.TLS.ACME.Email,
		DirectoryURL: getenv("ACME_DIRECTORY_URL", ""),
	}
	pm.Gen.HasCert = cm.HasCert
	// served backends (config + enabled services) → used to regenerate the vhosts.
	served := func() []models.Backend {
		merged := append([]models.Backend{}, bundle.Config.Backends...)
		if svc, err := config.LoadServices(resolvePath(*dir, bundle.Config.ServicesFile, "services.json")); err == nil {
			for _, b := range svc.Backends {
				if !b.Disabled {
					merged = append(merged, b)
				}
			}
		}
		return merged
	}
	cm.Reload = func() error { return pm.Apply(served()) }

	switch rest[0] {
	case "issue", "renew":
		if len(rest) < 2 {
			return errors.New("cert issue/renew requires a <host>")
		}
		host := rest[1]
		if *selfSigned {
			if err := cm.IssueSelfSigned(host); err != nil {
				return err
			}
			fmt.Println("self-signed certificate issued for", host)
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := cm.IssueACME(ctx, host); err != nil {
			return fmt.Errorf("acme issue %s: %w", host, err)
		}
		fmt.Println("certificate issued for", host)
		return nil
	case "list":
		seen := map[string]bool{}
		var hosts []string
		for _, b := range served() {
			if b.Host != "" && !seen[b.Host] {
				seen[b.Host] = true
				hosts = append(hosts, b.Host)
			}
		}
		for _, in := range cm.List(hosts) {
			src, exp := "-", "-"
			if in.Source != certmgr.SourceNone {
				src = string(in.Source)
				exp = in.NotAfter.Format("2006-01-02")
			}
			fmt.Printf("%-30s %-12s %s\n", in.Host, src, exp)
		}
		return nil
	default:
		return fmt.Errorf("unknown cert subcommand %q (use issue|renew|list)", rest[0])
	}
}

// runSetup creates the one-time setup profile (token + email) consumed by the
// web wizard, and prints the URL to open. See BLUEPRINT §13.
func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	email := fs.String("email", "", "email of the administrator profile to create")
	dir := fs.String("config", ".", "configuration directory")
	ttl := fs.Duration("ttl", 30*time.Minute, "validity of the setup token")
	_ = fs.Parse(args)

	cfg, err := config.LoadConfigOnly(*dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	em := strings.TrimSpace(*email)
	if em == "" {
		fmt.Print("Admin profile email: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		em = strings.TrimSpace(line)
	}
	if em == "" {
		return errors.New("missing email")
	}

	tok, err := randomToken()
	if err != nil {
		return err
	}
	st := models.SetupState{Token: tok, Email: em, ExpiresAt: time.Now().Add(*ttl)}
	setupPath := filepath.Join(*dir, "data", "setup.json")
	if err := config.SaveSetup(setupPath, st); err != nil {
		return fmt.Errorf("save setup: %w", err)
	}

	base := strings.TrimRight(cfg.Server.ExternalURL, "/")
	if base == "" {
		base = "http://localhost" + cfg.Server.Listen
	}
	d := *ttl
	fmt.Println("Setup profile created for:", em)
	fmt.Println("Open this URL to complete onboarding (expires in " + d.String() + "):")
	fmt.Println("  " + base + "/setup?token=" + tok)
	return nil
}

// runBackups lists the available snapshots.
func runBackups(args []string) error {
	fs := flag.NewFlagSet("backups", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	_ = fs.Parse(args)
	names, err := config.ListBackups(filepath.Join(*dir, "backups"))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("(no snapshots)")
		return nil
	}
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

// runRestore restores a snapshot back to its target file.
func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	snap := fs.String("snapshot", "", "snapshot name (see: xaltorka backups)")
	_ = fs.Parse(args)
	if *snap == "" {
		return errors.New("specify --snapshot=<name> (list with: xaltorka backups)")
	}
	target, err := config.RestoreSnapshot(*dir, *snap)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %s → %s. Apply by restarting the service (or reload for users/services).\n", *snap, target)
	return nil
}

// runUser creates or updates a user in users.json: sets password and/or the
// admin flag. Recovery/bootstrap from CLI:
//
//	xaltorka user --email a@b --password secret --admin   # create/promote admin
func runUser(args []string) error {
	fs := flag.NewFlagSet("user", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	email := fs.String("email", "", "user email")
	pw := fs.String("password", "", "set the password (local provider only)")
	prov := fs.String("provider", "local", "authentication provider: local|<oidc id> (e.g. google, microsoft)")
	admin := fs.Bool("admin", false, "grant the admin flag")
	noAdmin := fs.Bool("no-admin", false, "revoke the admin flag")
	_ = fs.Parse(args)
	if *email == "" {
		return errors.New("user requires --email")
	}
	provSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "provider" {
			provSet = true
		}
	})

	cfg, err := config.LoadConfigOnly(*dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	usersPath := resolvePath(*dir, cfg.UsersFile, "users.json")
	users, err := config.LoadUsers(usersPath)
	if err != nil {
		return err
	}

	idx := -1
	for i := range users.Users {
		if users.Users[i].Email == *email {
			idx = i
		}
	}
	if idx < 0 {
		if *prov == "local" && *pw == "" {
			return errors.New("new local user: --password is required (for OIDC use --provider <id>)")
		}
		users.Users = append(users.Users, models.User{Email: *email, Provider: *prov, Backends: []string{}})
		idx = len(users.Users) - 1
	}
	u := &users.Users[idx]
	if provSet {
		u.Provider = *prov
	}
	if *pw != "" {
		h, herr := auth.HashPassword(*pw)
		if herr != nil {
			return herr
		}
		u.PasswordHash = h
	}
	if *admin {
		u.Admin = true
	}
	if *noAdmin {
		u.Admin = false
	}
	if err := config.SaveUsers(usersPath, filepath.Join(*dir, "backups"), users); err != nil {
		return err
	}
	fmt.Printf("User %q updated (provider=%s, admin=%t). Restart the service to apply.\n", u.Email, u.Provider, u.Admin)
	return nil
}

// runAddLink appends an external link tile to services.json (dashboard-only).
func runAddLink(args []string) error {
	fs := flag.NewFlagSet("add-link", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	id := fs.String("id", "", "unique service id")
	name := fs.String("name", "", "name shown in the listing")
	link := fs.String("url", "", "external service URL")
	desc := fs.String("desc", "", "optional description")
	public := fs.Bool("public", false, "visible to all authenticated users")
	_ = fs.Parse(args)
	if *id == "" || *name == "" || *link == "" {
		return errors.New("add-link requires --id, --name and --url")
	}

	servicesPath, backupsDir, err := servicePaths(*dir)
	if err != nil {
		return err
	}
	svc, err := config.LoadServices(servicesPath)
	if err != nil {
		return err
	}
	if serviceIDExists(svc, *id) {
		return fmt.Errorf("id %q already present in services.json", *id)
	}
	svc.Links = append(svc.Links, models.Link{ID: *id, Name: *name, URL: *link, Description: *desc, Public: *public})
	if err := config.SaveServices(servicesPath, backupsDir, svc); err != nil {
		return err
	}
	fmt.Printf("Link '%s' added. Apply with: curl -X POST http://localhost/admin/reload (or restart).\n", *id)
	return nil
}

// runAddBackend appends a reverse-proxied service to services.json.
func runAddBackend(args []string) error {
	fs := flag.NewFlagSet("add-backend", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	id := fs.String("id", "", "unique backend id")
	name := fs.String("name", "", "name shown in the listing")
	host := fs.String("host", "", "public routed hostname")
	upstream := fs.String("upstream", "", "internal upstream, e.g. http://10.0.0.5:8080")
	rule := fs.String("rule", models.RuleAuthorized, "public|authenticated|authorized")
	publicURL := fs.String("url", "", "public URL for the tile (default: //host)")
	path := fs.String("path", "/", "path prefix")
	_ = fs.Parse(args)
	if *id == "" || *host == "" || *upstream == "" {
		return errors.New("add-backend requires --id, --host and --upstream")
	}

	servicesPath, backupsDir, err := servicePaths(*dir)
	if err != nil {
		return err
	}
	svc, err := config.LoadServices(servicesPath)
	if err != nil {
		return err
	}
	if serviceIDExists(svc, *id) {
		return fmt.Errorf("id %q already present in services.json", *id)
	}
	svc.Backends = append(svc.Backends, models.Backend{
		ID:     *id,
		Name:   *name,
		Host:   *host,
		URL:    *publicURL,
		Routes: []models.Route{{Path: *path, Rule: *rule, Upstream: *upstream}},
	})
	if err := config.SaveServices(servicesPath, backupsDir, svc); err != nil {
		return err
	}
	fmt.Printf("Backend '%s' (%s%s → %s) added. Apply by restarting the service.\n", *id, *host, *path, *upstream)
	return nil
}

// runAdminCIDR manages the admin IP whitelist (who may reach /admin) from the box,
// WITHOUT the web UI and WITHOUT being already whitelisted — the fix for the lockout
// a stale/narrow ADMIN_CIDR causes. It edits services.json's admin_ip_whitelist (the
// runtime override of config.admin.ip_whitelist) and, with --apply, hot-reloads the
// running gateway via SIGHUP (no restart, no HTTP, no IP gate). Ops:
//
//	xaltorka admin-cidr show               print config base + override + effective
//	xaltorka admin-cidr set <cidr...>      replace the override
//	xaltorka admin-cidr add <cidr...>      append to the override
//	xaltorka admin-cidr open               shortcut for set 0.0.0.0/0
//	xaltorka admin-cidr clear              drop the override (revert to config)
func runAdminCIDR(args []string) error {
	fs := flag.NewFlagSet("admin-cidr", flag.ExitOnError)
	dir := fs.String("config", ".", "configuration directory")
	apply := fs.Bool("apply", false, "hot-apply by sending SIGHUP to the running gateway")
	pid := fs.Int("pid", 1, "gateway PID to SIGHUP on --apply (in Docker the server is PID 1)")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("admin-cidr requires: show | set <cidr...> | add <cidr...> | open | clear")
	}
	op, cidrArgs := rest[0], rest[1:]

	servicesPath, backupsDir, err := servicePaths(*dir)
	if err != nil {
		return err
	}
	cfg, _ := config.LoadConfigOnly(*dir)
	svc, err := config.LoadServices(servicesPath)
	if err != nil {
		return err
	}
	effective := func() []string {
		if len(svc.AdminIPWhitelist) == 0 {
			return cfg.Admin.IPWhitelist
		}
		return svc.AdminIPWhitelist
	}

	switch op {
	case "show":
		fmt.Printf("config.admin.ip_whitelist  : %v\n", cfg.Admin.IPWhitelist)
		fmt.Printf("services.admin_ip_whitelist: %v\n", svc.AdminIPWhitelist)
		fmt.Printf("effective                  : %v\n", effective())
		if adminCIDRsOpen(effective()) {
			fmt.Println("state: /admin is IP-OPEN (0.0.0.0/0 or ::/0)")
		}
		return nil
	case "set", "add", "open":
		var incoming []string
		if op == "open" {
			incoming = []string{"0.0.0.0/0"}
		} else {
			if incoming, err = parseCIDRList(cidrArgs); err != nil {
				return err
			}
			if len(incoming) == 0 {
				return fmt.Errorf("admin-cidr %s requires at least one CIDR/IP", op)
			}
		}
		if op == "add" {
			svc.AdminIPWhitelist = dedupCIDRs(append(append([]string{}, svc.AdminIPWhitelist...), incoming...))
		} else {
			svc.AdminIPWhitelist = dedupCIDRs(incoming)
		}
	case "clear":
		svc.AdminIPWhitelist = nil
	default:
		return fmt.Errorf("unknown admin-cidr op %q (show|set|add|open|clear)", op)
	}

	if err := config.SaveServices(servicesPath, backupsDir, svc); err != nil {
		return err
	}
	fmt.Printf("services.admin_ip_whitelist now: %v (effective: %v)\n", svc.AdminIPWhitelist, effective())
	if adminCIDRsOpen(effective()) {
		fmt.Println("WARNING: /admin is now IP-OPEN (0.0.0.0/0) — enable 2FA/TOTP as the compensating control.")
	}
	if *apply {
		if err := syscall.Kill(*pid, syscall.SIGHUP); err != nil {
			return fmt.Errorf("saved, but hot-apply failed: SIGHUP pid %d: %w (reload manually or restart)", *pid, err)
		}
		fmt.Printf("applied: SIGHUP → pid %d (hot reload, no restart)\n", *pid)
	} else {
		fmt.Println("not applied — hot-apply with --apply (SIGHUP), or restart the gateway.")
	}
	return nil
}

// parseCIDRList validates space/comma/semicolon-separated IPs or CIDRs; a bare IP
// gets /32 (v4) or /128 (v6). Mirrors the admin-form CIDR normalizer.
func parseCIDRList(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		for _, tok := range strings.FieldsFunc(a, func(r rune) bool {
			return r == ',' || r == ' ' || r == ';' || r == '\t' || r == '\n' || r == '\r'
		}) {
			c := tok
			if !strings.Contains(c, "/") {
				if ip := net.ParseIP(c); ip != nil {
					if ip.To4() != nil {
						c += "/32"
					} else {
						c += "/128"
					}
				}
			}
			if _, _, err := net.ParseCIDR(c); err != nil {
				return nil, fmt.Errorf("invalid CIDR/IP %q", tok)
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// dedupCIDRs removes duplicates, preserving first-seen order.
func dedupCIDRs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, c := range in {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// adminCIDRsOpen reports whether the whitelist allows any address.
func adminCIDRsOpen(cidrs []string) bool {
	for _, c := range cidrs {
		if c == "0.0.0.0/0" || c == "::/0" {
			return true
		}
	}
	return false
}

// servicePaths resolves services.json and the backups dir for a config dir.
func servicePaths(dir string) (servicesPath, backupsDir string, err error) {
	cfg, err := config.LoadConfigOnly(dir)
	if err != nil {
		return "", "", fmt.Errorf("load config: %w", err)
	}
	return resolvePath(dir, cfg.ServicesFile, "services.json"), filepath.Join(dir, "backups"), nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func serviceIDExists(svc models.Services, id string) bool {
	for _, b := range svc.Backends {
		if b.ID == id {
			return true
		}
	}
	for _, l := range svc.Links {
		if l.ID == id {
			return true
		}
	}
	return false
}

func runHashPW(args []string) error {
	var pw string
	if len(args) > 0 {
		pw = args[0]
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("read password from stdin: %w", err)
		}
		pw = strings.TrimRight(line, "\r\n")
	}
	if pw == "" {
		return errors.New("empty password")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	fmt.Println(h)
	return nil
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// resolvePath joins p to dir unless p is absolute; empty p falls back to def.
func resolvePath(dir, p, def string) string {
	if p == "" {
		p = def
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
