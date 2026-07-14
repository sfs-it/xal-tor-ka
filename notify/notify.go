// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package notify is the shared outbound-message transport (Telegram, email) used by
// health alerts and by the remote-control channel. Sends are fire-and-forget: errors
// are logged, never returned into a request path (fail-open on notification is fine —
// a lost notification must not break the gateway).
package notify

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

// SendTelegram posts text to a Telegram chat via the Bot API.
func SendTelegram(token, chatID, text string) {
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	form := url.Values{"chat_id": {chatID}, "text": {text}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, strings.NewReader(form.Encode()))
	if err != nil {
		slog.Error("telegram send build failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("telegram send failed", "err", err)
		return
	}
	resp.Body.Close()
}

// SendEmail sends a plaintext email via SMTP (STARTTLS on :587 by SendMail default).
func SendEmail(cfg models.EmailCfg, sec models.SMTPSecret, subject, text string) {
	host := cfg.SMTPHost
	addr := host
	if !strings.Contains(host, ":") {
		addr = host + ":587"
	}
	host = strings.Split(host, ":")[0]
	var auth smtp.Auth
	if sec.Username != "" {
		auth = smtp.PlainAuth("", sec.Username, sec.Password, host)
	}
	body := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\n\r\n%s\r\n",
		subject, cfg.From, strings.Join(cfg.To, ", "), text)
	if err := smtp.SendMail(addr, auth, cfg.From, cfg.To, []byte(body)); err != nil {
		slog.Error("email send failed", "err", err)
	}
}

// Notifier delivers a message to every configured outbound channel.
type Notifier interface{ Send(subject, text string) }

type multi []Notifier

func (m multi) Send(subject, text string) {
	for _, n := range m {
		n.Send(subject, text)
	}
}

type tgNotifier struct{ token, chatID string }

func (t tgNotifier) Send(subject, text string) {
	msg := text
	if subject != "" {
		msg = subject + "\n" + text
	}
	SendTelegram(t.token, t.chatID, msg)
}

type emailNotifier struct {
	cfg models.EmailCfg
	sec models.SMTPSecret
}

func (e emailNotifier) Send(subject, text string) { SendEmail(e.cfg, e.sec, subject, text) }

// New builds a Notifier over the same channels configured for alerting (Telegram chat,
// email recipients). Returns a no-op notifier if nothing is configured.
func New(a models.AlertingCfg, sec models.Secrets) Notifier {
	var m multi
	if a.Telegram.Enabled && sec.Telegram.BotToken != "" && a.Telegram.ChatID != "" {
		m = append(m, tgNotifier{sec.Telegram.BotToken, a.Telegram.ChatID})
	}
	if a.Email.Enabled && a.Email.SMTPHost != "" && len(a.Email.To) > 0 {
		m = append(m, emailNotifier{a.Email, sec.SMTP})
	}
	return m
}
