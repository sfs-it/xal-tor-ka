package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xaltorka/auth"
	"xaltorka/config"
	"xaltorka/dockerscan"
	"xaltorka/health"
	"xaltorka/models"
	"xaltorka/version"
)

// Admin panel (BLUEPRINT §9). IP-whitelisted. Gestisce i servizi runtime
// (services.json: backend extra + link) e gli utenti (users.json), con
// persistenza atomica + snapshot + reload. I backend di config.json sono
// read-only (infrastruttura, env-templated).

const adminDocOpen = `<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Admin</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>`

// adminTopbar renders the shared admin header with the active nav item highlighted.
func adminTopbar(active string) string {
	items := []struct{ key, href, label string }{
		{"servizi", "/admin/servizi", "Servizi"},
		{"docker", "/admin/docker", "Docker"},
		{"utenti", "/admin/utenti", "Utenti"},
		{"monitoring", "/admin/monitoring", "Monitoring"},
	}
	var nav strings.Builder
	for _, i := range items {
		cls := ""
		if i.key == active {
			cls = ` class="active"`
		}
		fmt.Fprintf(&nav, `<a href="%s"%s>%s</a>`, i.href, cls, i.label)
	}
	return `<header class="topbar"><div class="brand"><a href="/admin" style="color:inherit;text-decoration:none">⛬ Xal-Tor-Ka</a><span class="sub">Amministrazione</span><span class="ver">` + version.Version + `</span></div><nav class="topnav">` +
		nav.String() +
		`<a href="/listing">Dashboard</a><form class="inline" method="post" action="/logout"><button class="btn sm">Esci</button></form></nav></header>`
}

// renderAdminPage writes the shared chrome (head + topbar + container) around a
// page-specific content template.
func (s *Server) renderAdminPage(w http.ResponseWriter, active string, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, adminDocOpen)
	io.WriteString(w, adminTopbar(active))
	io.WriteString(w, `<main class="container">`)
	_ = t.Execute(w, data)
	io.WriteString(w, `</main></body></html>`)
}

var overviewTmpl = template.Must(template.New("ov").Parse(`<h1>Amministrazione</h1>
<div class="grid">
 <a class="card" href="/admin/servizi"><div class="row"><h3>Servizi</h3><span class="tag">{{.Services}}</span></div><div class="meta">{{.ConfigBackends}} da config · {{.Links}} link</div></a>
 <a class="card" href="/admin/docker"><div class="row"><h3>Docker</h3><span class="tag">scopri</span></div><div class="meta">container attivi e porte host</div></a>
 <a class="card" href="/admin/utenti"><div class="row"><h3>Utenti</h3><span class="tag">{{.Users}}</span></div><div class="meta">utenti e autorizzazioni</div></a>
 <a class="card" href="/admin/monitoring"><div class="row"><h3>Monitoring</h3><span class="badge up">{{.Up}}</span> <span class="badge down">{{.Down}}</span></div><div class="meta">stato dei backend</div></a>
</div>`))

var servicesTmpl = template.Must(template.New("services").Parse(`<section>
 <h2>Servizi reverse-proxy</h2>
 <p class="hint">I servizi da <code>config.json</code> sono di sola lettura (infrastruttura). Quelli aggiunti qui vivono in <code>services.json</code>.</p>
 <div class="grid">
 {{range .ConfigBackends}}<div class="card"><div class="row"><h3>{{.ID}}</h3><span class="tag ro">config</span></div>
   <div class="meta"><a href="//{{.Host}}" target="_blank" rel="noopener">{{.Host}} ↗</a></div><div class="meta">{{range .Routes}}{{.Path}} → {{.Rule}}<br>{{end}}</div></div>{{end}}
 {{range .ServiceBackends}}<div class="card{{if .Disabled}} off{{end}}"><div class="row"><h3>{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}</h3><span class="tag">proxy</span>{{if .Disabled}} <span class="tag ro">off</span>{{end}}</div>
   <div class="meta"><a href="//{{.Host}}" target="_blank" rel="noopener">{{.Host}} ↗</a></div>
   {{if .Description}}<div class="meta">{{.Description}}</div>{{end}}
   <div class="meta">{{range .Routes}}{{.Path}} → {{.Rule}} → {{.Upstream}}<br>{{end}}</div>
   <div class="actions">
    <a class="btn sm" href="/admin/backend/edit?id={{.ID}}">edit</a>
    <form class="inline" method="post" action="/admin/backend/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Disabled}}abilita{{else}}disabilita{{end}}</button></form>
    <form class="inline" method="post" action="/admin/backend/del" onsubmit="return confirm('Eliminare {{.ID}}?')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">elimina</button></form>
   </div></div>{{end}}
 </div>
 <div class="card addcard" style="margin-top:1rem"><h3>Aggiungi servizio proxato</h3>
  <form method="post" action="/admin/backend/add"><div class="formgrid">
   <div><label>id</label><input name="id" required></div>
   <div><label>nome</label><input name="name"></div>
   <div><label>host</label><input name="host" placeholder="app.dominio.it" required></div>
   <div><label>path</label><input name="path" value="/"></div>
   <div><label>regola</label><select name="rule"><option>whitelist</option><option>authenticated</option><option>public</option></select></div>
   <div><label>upstream</label><input name="upstream" placeholder="http://10.0.0.5:8080"></div>
   <div><label>url pubblico</label><input name="url" placeholder="https://app.dominio.it"></div>
   <div><button class="btn primary">aggiungi</button></div>
  </div></form></div>
</section>
<section>
 <h2>Link esterni</h2><p class="hint">Riquadri nella dashboard, non proxati.</p>
 <div class="grid">
 {{range .Links}}<div class="card{{if .Disabled}} off{{end}}"><div class="row"><h3>{{.Name}}</h3><span class="tag ext">{{if .Public}}pubblico{{else}}riservato{{end}}</span>{{if .Disabled}} <span class="tag ro">off</span>{{end}}</div>
   <div class="meta"><a href="{{.URL}}" target="_blank" rel="noopener">{{.URL}} ↗</a></div>{{if .Description}}<div class="meta">{{.Description}}</div>{{end}}
   <div class="actions">
    <form class="inline" method="post" action="/admin/link/toggle"><input type="hidden" name="id" value="{{.ID}}"><button class="btn sm">{{if .Disabled}}abilita{{else}}disabilita{{end}}</button></form>
    <form class="inline" method="post" action="/admin/link/del" onsubmit="return confirm('Eliminare {{.ID}}?')"><input type="hidden" name="id" value="{{.ID}}"><button class="btn danger sm">elimina</button></form>
   </div></div>{{end}}
 </div>
 <div class="card addcard" style="margin-top:1rem"><h3>Aggiungi link</h3>
  <form method="post" action="/admin/link/add"><div class="formgrid">
   <div><label>id</label><input name="id" required></div>
   <div><label>nome</label><input name="name" required></div>
   <div><label>url</label><input name="url" placeholder="https://..." required></div>
   <div><label>descrizione</label><input name="desc"></div>
   <div><label class="check"><input type="checkbox" name="public"> pubblico</label></div>
   <div><button class="btn primary">aggiungi</button></div>
  </div></form></div>
</section>`))

var dockerTmpl = template.Must(template.New("docker").Parse(`<section>
 <h2>Scopri container Docker</h2>
 {{if .DockerEnabled}}
  <p class="hint">Container attivi con porte pubblicate. «Aggiungi» crea un vhost <code>&lt;nome&gt;.localhost</code> → <code>host.docker.internal:&lt;porta&gt;</code>.</p>
  <table><thead><tr><th>container</th><th>porta</th><th>vhost proposto</th><th>azione</th></tr></thead><tbody>
  {{range .Discovered}}<tr><td>{{.Name}}</td><td>{{.Port}}</td><td>{{.Host}}</td>
   <td>{{if .Added}}<span class="tag ro">già aggiunto</span>{{else}}<form class="inline" method="post" action="/admin/discover/add">
    <input type="hidden" name="name" value="{{.Name}}"><input type="hidden" name="port" value="{{.Port}}">
    <select name="rule"><option>whitelist</option><option>authenticated</option><option>public</option></select>
    <button class="btn primary sm">aggiungi</button></form>{{end}}</td></tr>
  {{else}}<tr><td colspan="4" class="empty">nessun container con porte pubblicate</td></tr>{{end}}
  </tbody></table>
 {{else}}<p class="hint">Scoperta Docker non attiva (variabile <code>DOCKER_PROXY</code> non impostata).</p>{{end}}
 <h3 style="margin-top:1.4rem">Porte host (localhost)</h3>
 <p class="hint">Trova porte in ascolto sull'host (es. tunnel PuTTY/SSH verso server remoti) da esporre come vhost.</p>
 <form method="get" action="/admin/hostscan">
  <label class="check">da <input name="from" value="3000" style="width:5.5rem"></label>
  <label class="check">a <input name="to" value="3100" style="width:5.5rem"></label>
  <button class="btn sm">scansiona</button>
 </form>
</section>`))

var usersTmpl = template.Must(template.New("users").Parse(`<section>
 <h2>Utenti</h2>
 <table><thead><tr><th>email</th><th></th><th>host abilitati</th><th></th></tr></thead><tbody>
 {{range .Users}}<tr>
  <td><a href="/admin/utenti/{{.Email}}">{{.Email}}</a></td>
  <td>{{if .Admin}}<span class="tag">admin</span>{{end}}</td>
  <td>{{if .Admin}}<span class="hint">tutti (admin)</span>{{else if .Hosts}}<details><summary>{{len .Hosts}} host</summary><ul class="hostlist">{{range .Hosts}}<li>{{.}}</li>{{end}}</ul></details>{{else}}<span class="hint">nessuno</span>{{end}}</td>
  <td><div class="actions">
   <a class="btn sm" href="/admin/utenti/{{.Email}}">proprietà</a>
   <form class="inline" method="post" action="/admin/user/del" onsubmit="return confirm('Eliminare {{.Email}}?')"><input type="hidden" name="email" value="{{.Email}}"><button class="btn danger sm">elimina</button></form>
  </div></td></tr>{{end}}
 </tbody></table>
 <div class="card addcard" style="margin-top:1rem"><h3>Crea utente locale</h3>
  <form method="post" action="/admin/user/add">
   <div class="formgrid">
    <div><label>email</label><input type="email" name="email" required></div>
    <div><label>password</label><input type="password" name="password" required></div>
   </div>
   <div class="checks"><label>autorizzazioni</label>{{range .AllIDs}}<label class="check"><input type="checkbox" name="authz" value="{{.}}">{{.}}</label>{{end}}</div>
   <div class="actions" style="justify-content:flex-start"><button class="btn primary">crea utente</button></div>
  </form></div>
</section>`))

var userDetailTmpl = template.Must(template.New("userdetail").Parse(`<section>
 <p><a href="/admin/utenti">← Utenti</a></p>
 <h2>Proprietà di «{{.Email}}»</h2>
 <div class="card">
  <div class="formgrid">
   <div><label>email</label><form class="inline" method="post" action="/admin/user/email"><input type="hidden" name="old" value="{{.Email}}"><input name="email" value="{{.Email}}"><button class="btn sm">salva</button></form></div>
   <div><label>provider</label><div style="padding-top:.4rem">{{.Provider}}{{if .Admin}} · <span class="tag">admin</span>{{end}}</div></div>
  </div>
  <div class="actions" style="justify-content:flex-start;margin-top:.8rem">
   <form class="inline" method="post" action="/admin/user/admin"><input type="hidden" name="email" value="{{.Email}}"><button class="btn sm">{{if .Admin}}togli admin{{else}}rendi admin{{end}}</button></form>
   <form class="inline" method="post" action="/admin/user/password"><input type="hidden" name="email" value="{{.Email}}"><input type="password" name="password" placeholder="nuova password" style="width:11rem"><button class="btn sm">imposta password</button></form>
   <form class="inline" method="post" action="/admin/user/totp"><input type="hidden" name="email" value="{{.Email}}"><button class="btn sm">reset 2FA</button></form>
   <form class="inline" method="post" action="/admin/user/del" onsubmit="return confirm('Eliminare {{.Email}}?')"><input type="hidden" name="email" value="{{.Email}}"><button class="btn danger sm">elimina utente</button></form>
  </div>
 </div>
 <div class="card" style="margin-top:1rem"><h3>Autorizzazioni (host abilitati)</h3>
  {{if .Admin}}<p class="hint">Questo utente è amministratore: accede a tutti i servizi, la whitelist non si applica.</p>
  {{else}}<form method="post" action="/admin/user/authz"><input type="hidden" name="email" value="{{.Email}}">
   <div class="checks">{{range .AllIDs}}<label class="check"><input type="checkbox" name="authz" value="{{.}}" {{if index $.Checked .}}checked{{end}}>{{.}}</label>{{else}}<span class="hint">nessun servizio configurato</span>{{end}}</div>
   <div class="actions" style="justify-content:flex-start;margin-top:.6rem"><button class="btn primary">salva autorizzazioni</button></div>
  </form>{{end}}
 </div>
</section>`))

var monitoringTmpl = template.Must(template.New("mon").Parse(`<section>
 <h2>Monitoring backend</h2>
 <table><thead><tr><th>id</th><th>host</th><th>stato</th><th>ultimo errore</th><th>ultimo check</th></tr></thead><tbody>
 {{range .Monitoring}}<tr><td>{{.BackendID}}</td><td><a href="//{{.Host}}" target="_blank" rel="noopener">{{.Host}} ↗</a></td><td><span class="badge {{.State}}">{{.State}}</span></td><td>{{.LastError}}</td><td>{{.LastCheck.Format "15:04:05"}}</td></tr>
 {{else}}<tr><td colspan="5" class="empty">nessun health check configurato/eseguito</td></tr>{{end}}
 </tbody></table>
</section>`))

var adminEditTmpl = template.Must(template.New("adminedit").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Modifica servizio</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<header class="topbar"><div class="brand">⛬ Xal-Tor-Ka<span class="sub">Modifica servizio</span></div>
 <nav class="topnav"><a href="/admin/servizi">← Servizi</a></nav></header>
<main class="container">
 <h1>Modifica «{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}}»</h1>
 <div class="card">
  <form method="post" action="/admin/backend/edit">
   <input type="hidden" name="id" value="{{.ID}}">
   <div class="formgrid">
    <div><label>id (non modificabile)</label><input value="{{.ID}}" disabled></div>
    <div><label>nome</label><input name="name" value="{{.Name}}"></div>
    <div><label>host</label><input name="host" value="{{.Host}}" required></div>
    <div><label>url pubblico</label><input name="url" value="{{.URL}}"></div>
    <div><label>path</label><input name="path" value="{{.Path}}"></div>
    <div><label>regola</label><select name="rule">
     <option {{if eq .Rule "whitelist"}}selected{{end}}>whitelist</option>
     <option {{if eq .Rule "authenticated"}}selected{{end}}>authenticated</option>
     <option {{if eq .Rule "public"}}selected{{end}}>public</option></select></div>
    <div><label>upstream</label><input name="upstream" value="{{.Upstream}}" required></div>
   </div>
   <div style="margin-top:.6rem"><label>descrizione</label><input name="description" value="{{.Description}}"></div>
   <div class="actions" style="justify-content:flex-start;margin-top:1rem">
    <button class="btn primary">salva</button><a class="btn" href="/admin/servizi">annulla</a></div>
  </form>
 </div>
</main></body></html>`))

var adminQRTmpl = template.Must(template.New("adminqr").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · 2FA</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<div class="auth-wrap"><div class="auth-card qr">
 <h1>2FA per {{.Email}}</h1>
 <p class="hint">Fai scansionare il QR con l'app authenticator dell'utente, oppure passagli la chiave.</p>
 <p><img src="{{.QR}}" alt="QR otpauth" width="240" height="240"></p>
 <p>Chiave: <code>{{.Secret}}</code></p>
 <p style="margin-top:1.2rem"><a href="/admin/utenti">← torna agli utenti</a></p>
</div></div></body></html>`))

// handleAdmin is the overview page with summary tiles linking to the sections.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	up, down := 0, 0
	if s.Health != nil {
		for _, st := range s.Health.Snapshot() {
			if st.State == health.StateUp {
				up++
			} else {
				down++
			}
		}
	}
	s.renderAdminPage(w, "", overviewTmpl, struct {
		Services, Links, Users, ConfigBackends, Up, Down int
	}{len(svc.Backends), len(svc.Links), s.Users.Count(), len(s.BaseBackends), up, down})
}

func (s *Server) handleAdminServices(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	s.renderAdminPage(w, "servizi", servicesTmpl, struct {
		ConfigBackends  []models.Backend
		ServiceBackends []models.Backend
		Links           []models.Link
	}{s.BaseBackends, svc.Backends, svc.Links})
}

func (s *Server) handleAdminDocker(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	s.renderAdminPage(w, "docker", dockerTmpl, struct {
		DockerEnabled bool
		Discovered    []discoveredRow
	}{s.DockerProxyURL != "", s.discover(r, svc)})
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	users := s.Users.All()
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	s.renderAdminPage(w, "utenti", usersTmpl, struct {
		Users  []adminUserRow
		AllIDs []string
	}{rowsFor(users), s.allServiceIDs(svc)})
}

// handleAdminUserDetail is the per-user properties page.
func (s *Server) handleAdminUserDetail(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PathValue("email")
	u, found := s.Users.Get(email)
	if !found {
		http.Redirect(w, r, "/admin/utenti", http.StatusSeeOther)
		return
	}
	svc, _ := config.LoadServices(s.ServicesPath)
	checked := map[string]bool{}
	for _, b := range u.Backends {
		checked[b] = true
	}
	s.renderAdminPage(w, "utenti", userDetailTmpl, struct {
		Email, Provider string
		Admin           bool
		AllIDs          []string
		Checked         map[string]bool
	}{u.Email, u.Provider, u.Admin, s.allServiceIDs(svc), checked})
}

func (s *Server) handleAdminMonitoring(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	var monitoring []health.Status
	if s.Health != nil {
		monitoring = s.Health.Snapshot()
		sort.Slice(monitoring, func(i, j int) bool { return monitoring[i].BackendID < monitoring[j].BackendID })
	}
	s.renderAdminPage(w, "monitoring", monitoringTmpl, struct{ Monitoring []health.Status }{monitoring})
}

type discoveredRow struct {
	Name  string
	Port  int
	Host  string
	Added bool
}

// discover queries the docker-socket-proxy and proposes vhosts for running
// containers with published TCP ports (excluding our own stack). Best-effort:
// returns nil on any error or when disabled.
func (s *Server) discover(r *http.Request, svc models.Services) []discoveredRow {
	if s.DockerProxyURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	containers, err := dockerscan.List(ctx, s.DockerProxyURL)
	if err != nil {
		return nil
	}
	existing := map[string]bool{}
	for _, b := range s.BaseBackends {
		existing[b.Host] = true
	}
	for _, b := range svc.Backends {
		existing[b.Host] = true
	}

	seen := map[string]bool{}
	var out []discoveredRow
	for _, c := range containers {
		if excluded(c.Name, s.DockerExclude) {
			continue
		}
		name := sanitizeName(c.Name)
		if name == "" {
			continue
		}
		for _, p := range c.Ports {
			if p.Type != "tcp" || p.PublicPort == 0 {
				continue
			}
			host := name + ".localhost"
			key := fmt.Sprintf("%s:%d", host, p.PublicPort)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, discoveredRow{Name: name, Port: p.PublicPort, Host: host, Added: existing[host]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// handleDiscoverAdd creates a service backend from a discovered container,
// routed via host.docker.internal:<published-port>.
func (s *Server) handleDiscoverAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	name := sanitizeName(r.PostFormValue("name"))
	port, _ := strconv.Atoi(r.PostFormValue("port"))
	rule := r.PostFormValue("rule")
	if name == "" || port <= 0 || port > 65535 {
		http.Error(w, "container/porta non validi", http.StatusBadRequest)
		return
	}
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		rule = "whitelist"
	}
	host := name + ".localhost"
	upstream := fmt.Sprintf("http://host.docker.internal:%d", port)
	err := s.mutateServices(func(svc *models.Services) error {
		if s.idTaken(*svc, name) {
			return fmt.Errorf("id %q già esistente", name)
		}
		svc.Backends = append(svc.Backends, models.Backend{
			ID: name, Name: name, Host: host, URL: "//" + host,
			Routes: []models.Route{{Path: "/", Rule: rule, Upstream: upstream}},
			Health: models.Health{URL: upstream + "/", IntervalSeconds: 30, TimeoutSeconds: 5},
		})
		return nil
	})
	s.afterMutation(w, r, err)
}

var hostScanTmpl = template.Must(template.New("hostscan").Parse(`<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xal-Tor-Ka · Porte host</title><link rel="stylesheet" href="/assets/admin.css"><script src="/assets/admin.js" defer></script></head><body>
<header class="topbar"><div class="brand">⛬ Xal-Tor-Ka<span class="sub">Porte host</span></div>
 <nav class="topnav"><a href="/admin/servizi">← Servizi</a></nav></header>
<main class="container">
 <h1>Porte in ascolto su host ({{.From}}–{{.To}})</h1>
 <p class="hint">Porte raggiungibili via <code>host.docker.internal</code> (es. tunnel PuTTY/SSH, servizi host). Spunta le porte e aggiungile in blocco; nome vuoto → <code>host-&lt;porta&gt;</code>.</p>
 <form method="post" action="/admin/hostscan/add">
  <table><thead><tr>
   <th><input type="checkbox" onclick="for(const c of document.querySelectorAll('input[name=ports]'))c.checked=this.checked"></th>
   <th>porta</th><th>nome vhost</th><th>stato</th></tr></thead><tbody>
  {{range .Ports}}<tr>
   <td>{{if not .Added}}<input type="checkbox" name="ports" value="{{.Port}}">{{end}}</td>
   <td>{{.Port}}</td>
   <td>{{if .Added}}<span class="tag ro">già: {{.ExistingHost}}</span>{{else}}<input name="name_{{.Port}}" placeholder="host-{{.Port}}">{{end}}</td>
   <td>{{if .Added}}—{{else}}nuovo{{end}}</td></tr>
  {{else}}<tr><td colspan="4" class="empty">nessuna porta aperta nell'intervallo</td></tr>{{end}}
  </tbody></table>
  <div class="actions" style="justify-content:flex-start;margin-top:.8rem">
   <label>regola <select name="rule"><option>whitelist</option><option>authenticated</option><option>public</option></select></label>
   <button class="btn primary">Aggiungi selezionati</button>
  </div>
 </form>
 <p style="margin-top:1rem"><a class="btn" href="/admin">← torna all'amministrazione</a></p>
</main></body></html>`))

type hostPortRow struct {
	Port         int
	Added        bool
	ExistingHost string
}

// handleHostScan scans host.docker.internal on the requested port range and
// lists open ports to be turned into vhosts.
func (s *Server) handleHostScan(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	to, _ := strconv.Atoi(r.URL.Query().Get("to"))
	if from <= 0 {
		from = 3000
	}
	if to <= 0 {
		to = from + 100
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	open := dockerscan.ScanPorts(ctx, "host.docker.internal", from, to)

	svc, _ := config.LoadServices(s.ServicesPath)
	byPort := map[int]string{}
	collect := func(bs []models.Backend) {
		for _, b := range bs {
			for _, rt := range b.Routes {
				if p := hostInternalPort(rt.Upstream); p > 0 {
					byPort[p] = b.Host
				}
			}
		}
	}
	collect(s.BaseBackends)
	collect(svc.Backends)

	rows := make([]hostPortRow, 0, len(open))
	for _, p := range open {
		h, added := byPort[p]
		rows = append(rows, hostPortRow{Port: p, Added: added, ExistingHost: h})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = hostScanTmpl.Execute(w, struct {
		From, To int
		Ports    []hostPortRow
	}{From: from, To: to, Ports: rows})
}

// handleHostScanAdd bulk-creates vhosts for the selected host ports.
func (s *Server) handleHostScanAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	rule := r.PostFormValue("rule")
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		rule = "whitelist"
	}
	ports := r.PostForm["ports"]
	err := s.mutateServices(func(svc *models.Services) error {
		for _, ps := range ports {
			port, e := strconv.Atoi(ps)
			if e != nil || port < 1 || port > 65535 {
				continue
			}
			name := sanitizeName(r.PostFormValue("name_" + ps))
			if name == "" {
				name = fmt.Sprintf("host-%d", port)
			}
			if s.idTaken(*svc, name) {
				continue // salta duplicati senza fallire l'intero batch
			}
			host := name + ".localhost"
			upstream := fmt.Sprintf("http://host.docker.internal:%d", port)
			svc.Backends = append(svc.Backends, models.Backend{
				ID: name, Name: name, Host: host, URL: "//" + host,
				Routes: []models.Route{{Path: "/", Rule: rule, Upstream: upstream}},
				Health: models.Health{URL: upstream + "/", IntervalSeconds: 30, TimeoutSeconds: 5},
			})
		}
		return nil
	})
	s.afterMutation(w, r, err)
}

// handleUserEmail renames a user (email is the key).
func (s *Server) handleUserEmail(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	old := r.PostFormValue("old")
	neu := strings.TrimSpace(r.PostFormValue("email"))
	if neu == "" {
		http.Error(w, "email obbligatoria", http.StatusBadRequest)
		return
	}
	err := s.mutateUsers(func(users *[]models.User) error {
		for _, u := range *users {
			if u.Email == neu && neu != old {
				return fmt.Errorf("email %q già in uso", neu)
			}
		}
		for i := range *users {
			if (*users)[i].Email == old {
				(*users)[i].Email = neu
				return nil
			}
		}
		return fmt.Errorf("utente non trovato")
	})
	s.afterMutation(w, r, err)
}

// handleUserPassword sets a new password for a local user (admin-driven reset).
func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	pw := r.PostFormValue("password")
	if pw == "" {
		http.Error(w, "password obbligatoria", http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		http.Error(w, "errore interno", http.StatusInternalServerError)
		return
	}
	err = s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == email {
				(*users)[i].PasswordHash = hash
				return nil
			}
		}
		return fmt.Errorf("utente non trovato")
	})
	s.afterMutation(w, r, err)
}

// hostInternalize rewrites an upstream pointing at localhost/127.0.0.1 to
// host.docker.internal: dentro i container "localhost" è il container stesso,
// non l'host, quindi i servizi dell'host vanno raggiunti via host.docker.internal.
func hostInternalize(upstream string) string {
	i := strings.Index(upstream, "://")
	if i < 0 {
		return upstream
	}
	rest := upstream[i+3:]
	hostport, tail := rest, ""
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		hostport, tail = rest[:j], rest[j:]
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, ""
	}
	if host != "localhost" && host != "127.0.0.1" {
		return upstream
	}
	host = "host.docker.internal"
	hp := host
	if port != "" {
		hp = host + ":" + port
	}
	return upstream[:i+3] + hp + tail
}

// hostInternalPort returns the port of an upstream pointing at host.docker.internal,
// or 0 otherwise.
func hostInternalPort(upstream string) int {
	s := upstream
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host != "host.docker.internal" {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

// sanitizeName lowercases a container name and keeps only [a-z0-9-].
func sanitizeName(n string) string {
	n = strings.ToLower(strings.TrimPrefix(n, "/"))
	var b strings.Builder
	for _, c := range n {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func excluded(name string, deny []string) bool {
	ln := strings.ToLower(name)
	for _, d := range deny {
		if d != "" && strings.Contains(ln, strings.ToLower(d)) {
			return true
		}
	}
	return false
}

// adminUserRow precomputes per-(user,id) authorization to render checkboxes.
type adminUserRow struct {
	Email    string
	Provider string
	Admin    bool
	Hosts    []string
}

func rowsFor(users []models.User) []adminUserRow {
	rows := make([]adminUserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, adminUserRow{Email: u.Email, Provider: u.Provider, Admin: u.Admin, Hosts: u.Backends})
	}
	return rows
}

// handleUserAdmin toggles the admin flag, keeping at least one admin.
func (s *Server) handleUserAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	err := s.mutateUsers(func(users *[]models.User) error {
		target, admins := -1, 0
		for i := range *users {
			if (*users)[i].Admin {
				admins++
			}
			if (*users)[i].Email == email {
				target = i
			}
		}
		if target < 0 {
			return fmt.Errorf("utente non trovato")
		}
		if (*users)[target].Admin && admins <= 1 {
			return fmt.Errorf("deve restare almeno un amministratore")
		}
		(*users)[target].Admin = !(*users)[target].Admin
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) allServiceIDs(svc models.Services) []string {
	set := map[string]bool{}
	for _, b := range s.BaseBackends {
		set[b.ID] = true
	}
	for _, b := range svc.Backends {
		set[b.ID] = true
	}
	for _, l := range svc.Links {
		set[l.ID] = true
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// --- service mutations -------------------------------------------------------

func (s *Server) mutateServices(fn func(*models.Services) error) error {
	svc, err := config.LoadServices(s.ServicesPath)
	if err != nil {
		return err
	}
	if err := fn(&svc); err != nil {
		return err
	}
	if err := config.SaveServices(s.ServicesPath, s.BackupsDir, svc); err != nil {
		return err
	}
	return s.Reload()
}

func (s *Server) handleLinkAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id, name, url := r.PostFormValue("id"), r.PostFormValue("name"), r.PostFormValue("url")
	if id == "" || name == "" || url == "" {
		http.Error(w, "id, name, url obbligatori", http.StatusBadRequest)
		return
	}
	err := s.mutateServices(func(svc *models.Services) error {
		if s.idTaken(*svc, id) {
			return fmt.Errorf("id già esistente")
		}
		svc.Links = append(svc.Links, models.Link{ID: id, Name: name, URL: url,
			Description: r.PostFormValue("desc"), Public: r.PostFormValue("public") != ""})
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleLinkDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		out := svc.Links[:0]
		for _, l := range svc.Links {
			if l.ID != id {
				out = append(out, l)
			}
		}
		svc.Links = out
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id, host := r.PostFormValue("id"), r.PostFormValue("host")
	upstream := hostInternalize(r.PostFormValue("upstream"))
	rule := r.PostFormValue("rule")
	if id == "" || host == "" || upstream == "" {
		http.Error(w, "id, host, upstream obbligatori", http.StatusBadRequest)
		return
	}
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		http.Error(w, "regola non valida", http.StatusBadRequest)
		return
	}
	path := r.PostFormValue("path")
	if path == "" {
		path = "/"
	}
	err := s.mutateServices(func(svc *models.Services) error {
		if s.idTaken(*svc, id) {
			return fmt.Errorf("id già esistente")
		}
		svc.Backends = append(svc.Backends, models.Backend{
			ID: id, Name: r.PostFormValue("name"), Host: host, URL: r.PostFormValue("url"),
			Routes: []models.Route{{Path: path, Rule: rule, Upstream: upstream}},
		})
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		out := svc.Backends[:0]
		for _, b := range svc.Backends {
			if b.ID != id {
				out = append(out, b)
			}
		}
		svc.Backends = out
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendToggle(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Backends {
			if svc.Backends[i].ID == id {
				svc.Backends[i].Disabled = !svc.Backends[i].Disabled
				return nil
			}
		}
		return fmt.Errorf("backend non trovato")
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleLinkToggle(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Links {
			if svc.Links[i].ID == id {
				svc.Links[i].Disabled = !svc.Links[i].Disabled
				return nil
			}
		}
		return fmt.Errorf("link non trovato")
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleBackendEditForm(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	svc, _ := config.LoadServices(s.ServicesPath)
	for _, b := range svc.Backends {
		if b.ID != id {
			continue
		}
		rt := models.Route{Path: "/", Rule: "whitelist"}
		if len(b.Routes) > 0 {
			rt = b.Routes[0]
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = adminEditTmpl.Execute(w, struct {
			ID, Name, Description, Host, URL, Path, Rule, Upstream string
		}{b.ID, b.Name, b.Description, b.Host, b.URL, rt.Path, rt.Rule, rt.Upstream})
		return
	}
	http.Error(w, "backend non trovato", http.StatusNotFound)
}

func (s *Server) handleBackendEdit(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	id := r.PostFormValue("id")
	rule := r.PostFormValue("rule")
	if rule != "public" && rule != "authenticated" && rule != "whitelist" {
		rule = "whitelist"
	}
	path := r.PostFormValue("path")
	if path == "" {
		path = "/"
	}
	upstream := hostInternalize(r.PostFormValue("upstream"))
	if err := validateUpstream(upstream); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	host := r.PostFormValue("host")
	if host == "" {
		http.Error(w, "host obbligatorio", http.StatusBadRequest)
		return
	}
	err := s.mutateServices(func(svc *models.Services) error {
		for i := range svc.Backends {
			if svc.Backends[i].ID != id {
				continue
			}
			b := &svc.Backends[i]
			b.Name = r.PostFormValue("name")
			b.Description = r.PostFormValue("description")
			b.Host = host
			b.URL = r.PostFormValue("url")
			if len(b.Routes) == 0 {
				b.Routes = []models.Route{{}}
			}
			b.Routes[0] = models.Route{Path: path, Rule: rule, Upstream: upstream}
			return nil
		}
		return fmt.Errorf("backend non trovato")
	})
	s.afterMutation(w, r, err)
}

// validateUpstream checks an upstream URL has a valid host:port.
func validateUpstream(upstream string) error {
	s := upstream
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return fmt.Errorf("upstream non valido (atteso http://host:porta)")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("porta upstream non valida")
	}
	return nil
}

// --- user mutations ----------------------------------------------------------

func (s *Server) mutateUsers(fn func(*[]models.User) error) error {
	users := s.Users.All()
	if err := fn(&users); err != nil {
		return err
	}
	if err := config.SaveUsers(s.UsersPath, s.BackupsDir, models.Users{Users: users}); err != nil {
		return err
	}
	s.Users.Replace(users)
	return nil
}

func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email, pw := r.PostFormValue("email"), r.PostFormValue("password")
	if email == "" || pw == "" {
		http.Error(w, "email e password obbligatorie", http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		http.Error(w, "errore interno", http.StatusInternalServerError)
		return
	}
	secret := ""
	if !s.Cfg.DisableTOTP {
		secret, err = auth.NewTOTPSecret()
		if err != nil {
			http.Error(w, "errore interno", http.StatusInternalServerError)
			return
		}
	}
	authzIDs := r.PostForm["authz"]
	err = s.mutateUsers(func(users *[]models.User) error {
		for _, u := range *users {
			if u.Email == email {
				return fmt.Errorf("utente già esistente")
			}
		}
		*users = append(*users, models.User{
			Email: email, Provider: "local", PasswordHash: hash,
			TOTPSecret: secret, Backends: authzIDs,
		})
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Cfg.DisableTOTP {
		s.afterMutation(w, r, nil) // niente QR quando il 2FA è disattivato
		return
	}
	s.renderAdminQR(w, email, secret)
}

func (s *Server) handleUserDel(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	err := s.mutateUsers(func(users *[]models.User) error {
		out := (*users)[:0]
		for _, u := range *users {
			if u.Email != email {
				out = append(out, u)
			}
		}
		*users = out
		return nil
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleUserAuthz(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	ids := r.PostForm["authz"]
	err := s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == email {
				(*users)[i].Backends = ids
				return nil
			}
		}
		return fmt.Errorf("utente non trovato")
	})
	s.afterMutation(w, r, err)
}

func (s *Server) handleUserTOTP(w http.ResponseWriter, r *http.Request) {
	if !s.adminGuard(w, r) {
		return
	}
	email := r.PostFormValue("email")
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		http.Error(w, "errore interno", http.StatusInternalServerError)
		return
	}
	err = s.mutateUsers(func(users *[]models.User) error {
		for i := range *users {
			if (*users)[i].Email == email {
				(*users)[i].TOTPSecret = secret
				return nil
			}
		}
		return fmt.Errorf("utente non trovato")
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderAdminQR(w, email, secret)
}

// --- helpers -----------------------------------------------------------------

// afterMutation redirects back to /admin on success (PRG), else shows the error.
func (s *Server) afterMutation(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dest := "/admin/servizi"
	if ref := r.Referer(); ref != "" {
		if u, e := url.Parse(ref); e == nil && strings.HasPrefix(u.Path, "/admin") {
			dest = u.Path
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) idTaken(svc models.Services, id string) bool {
	for _, b := range s.BaseBackends {
		if b.ID == id {
			return true
		}
	}
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

func (s *Server) renderAdminQR(w http.ResponseWriter, email, secret string) {
	png, err := qrcode.Encode(otpauthURI(email, secret), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "errore QR", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminQRTmpl.Execute(w, struct {
		Email  string
		Secret string
		QR     template.URL
	}{Email: email, Secret: secret, QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))})
}
