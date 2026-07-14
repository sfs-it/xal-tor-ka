// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package health

import (
	"fmt"
	"log/slog"

	"xaltorka/models"
	"xaltorka/notify"
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
	notify.SendTelegram(t.token, t.chatID, message(cur, prev))
}

type emailAlerter struct {
	cfg models.EmailCfg
	sec models.SMTPSecret
}

func (e *emailAlerter) Notify(cur Status, prev State) {
	notify.SendEmail(e.cfg, e.sec, "Xal-Tor-Ka alert", message(cur, prev))
}
