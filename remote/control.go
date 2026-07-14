// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package remote is the inbound remote-control channel: it polls Telegram (and, later,
// an IMAP spool) for commands, authenticates the sender, dispatches a VETTED command
// from an allow-list, and replies. It holds no app state — the command handlers are
// closures supplied by main.go, keeping this package a thin, auditable transport.
//
// Security model (fail-closed): commands run ONLY from allow-listed Telegram chat IDs;
// only commands present in the supplied handler map (optionally further narrowed by
// config AllowCommands) execute; every received command is logged for audit.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"xaltorka/models"
	"xaltorka/notify"
)

// Controller drives the remote-control channels.
type Controller struct {
	cfg      models.RemoteControlCfg
	botToken string
	commands map[string]func(args string) string // vetted handlers, supplied by main.go
	log      *slog.Logger
}

// New builds a Controller. commands is the vetted handler map (read-only for now); a
// nil/empty map disables command dispatch (channel then only greets on start).
func New(cfg models.RemoteControlCfg, botToken string, commands map[string]func(string) string, log *slog.Logger) *Controller {
	if commands == nil {
		commands = map[string]func(string) string{}
	}
	return &Controller{cfg: cfg, botToken: botToken, commands: commands, log: log}
}

// Start launches the enabled channel pollers as goroutines bound to ctx.
func (c *Controller) Start(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	go c.telegramLoop(ctx)
	// email/IMAP+DKIM channel: added in a later increment.
}

func (c *Controller) telegramLoop(ctx context.Context) {
	if !c.cfg.Telegram.Enabled || c.botToken == "" || len(c.cfg.Telegram.AllowChatIDs) == 0 {
		return
	}
	interval := time.Duration(c.cfg.Telegram.PollSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	allow := map[string]bool{}
	for _, id := range c.cfg.Telegram.AllowChatIDs {
		allow[strings.TrimSpace(id)] = true
	}
	c.log.Info("remote-control: telegram command channel active", "chats", len(allow), "interval", interval)
	t := time.NewTicker(interval)
	defer t.Stop()
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			offset = c.pollTelegram(ctx, offset, allow)
		}
	}
}

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func (c *Controller) pollTelegram(ctx context.Context, offset int64, allow map[string]bool) int64 {
	api := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=0&offset=%d", c.botToken, offset)
	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, api, nil)
	if err != nil {
		return offset
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.log.Warn("remote-control: telegram getUpdates failed", "err", err)
		return offset
	}
	defer resp.Body.Close()
	var u struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil || !u.OK {
		return offset
	}
	for _, up := range u.Result {
		offset = up.UpdateID + 1 // ack: never re-process this update
		chatID := strconv.FormatInt(up.Message.Chat.ID, 10)
		if !allow[chatID] {
			c.log.Warn("remote-control: command from non-allowed telegram chat", "chat", chatID)
			continue
		}
		c.dispatch("telegram", chatID, up.Message.Text, func(reply string) {
			notify.SendTelegram(c.botToken, chatID, reply)
		})
	}
	return offset
}

// dispatch parses a command line, checks it against the allow-list, runs the vetted
// handler, and delivers the reply. Every command is audited.
func (c *Controller) dispatch(channel, sender, text string, reply func(string)) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	c.log.Info("remote-control: command", "channel", channel, "sender", sender, "cmd", cmd)
	reply(c.run(cmd, args))
}

func (c *Controller) run(cmd, args string) string {
	if cmd == "help" || cmd == "" {
		return c.helpText()
	}
	if len(c.cfg.AllowCommands) > 0 && !contains(c.cfg.AllowCommands, cmd) {
		return "command not allowed: " + cmd
	}
	h, ok := c.commands[cmd]
	if !ok {
		return "unknown command: " + cmd + "\n" + c.helpText()
	}
	return h(args)
}

func (c *Controller) helpText() string {
	names := make([]string, 0, len(c.commands)+1)
	names = append(names, "help")
	for k := range c.commands {
		if len(c.cfg.AllowCommands) == 0 || contains(c.cfg.AllowCommands, k) {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return "commands: " + strings.Join(names, ", ")
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
