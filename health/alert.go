package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"xaltorka/models"
)

// NewAlerter builds the alerter from the monitoring config + secrets. It always
// logs transitions; Telegram and email are added when enabled and configured.
func NewAlerter(cfg models.AlertingCfg, sec models.Secrets) Alerter {
	m := multiAlerter{logAlerter{}}
	if cfg.Telegram.Enabled && sec.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		m = append(m, &telegramAlerter{token: sec.Telegram.BotToken, chatID: cfg.Telegram.ChatID})
	}
	if cfg.Email.Enabled && cfg.Email.SMTPHost != "" && len(cfg.Email.To) > 0 {
		m = append(m, &emailAlerter{cfg: cfg.Email, sec: sec.SMTP})
	}
	return m
}

func message(cur Status, prev State) string {
	return fmt.Sprintf("[Xal-Tor-Ka] backend %q (%s): %s → %s%s",
		cur.BackendID, cur.Host, prev, cur.State, suffix(cur.LastError))
}

func suffix(e string) string {
	if e == "" {
		return ""
	}
	return " (" + e + ")"
}

type multiAlerter []Alerter

func (m multiAlerter) Notify(cur Status, prev State) {
	for _, a := range m {
		a.Notify(cur, prev)
	}
}

type logAlerter struct{}

func (logAlerter) Notify(cur Status, prev State) {
	slog.Warn("backend state change", "backend", cur.BackendID, "host", cur.Host,
		"from", string(prev), "to", string(cur.State), "error", cur.LastError)
}

type telegramAlerter struct {
	token  string
	chatID string
}

func (t *telegramAlerter) Notify(cur Status, prev State) {
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	form := url.Values{"chat_id": {t.chatID}, "text": {message(cur, prev)}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, strings.NewReader(form.Encode()))
	if err != nil {
		slog.Error("telegram alert build failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("telegram alert send failed", "err", err)
		return
	}
	resp.Body.Close()
}

type emailAlerter struct {
	cfg models.EmailCfg
	sec models.SMTPSecret
}

func (e *emailAlerter) Notify(cur Status, prev State) {
	host := e.cfg.SMTPHost
	addr := host
	if !strings.Contains(host, ":") {
		addr = host + ":587"
		host = strings.Split(host, ":")[0]
	} else {
		host = strings.Split(host, ":")[0]
	}
	var authMech smtp.Auth
	if e.sec.Username != "" {
		authMech = smtp.PlainAuth("", e.sec.Username, e.sec.Password, host)
	}
	body := fmt.Sprintf("Subject: Xal-Tor-Ka alert\r\nFrom: %s\r\nTo: %s\r\n\r\n%s\r\n",
		e.cfg.From, strings.Join(e.cfg.To, ", "), message(cur, prev))
	if err := smtp.SendMail(addr, authMech, e.cfg.From, e.cfg.To, []byte(body)); err != nil {
		slog.Error("email alert send failed", "err", err)
	}
}
