// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sort"
	"time"
)

const (
	maxRequest    = 64 * 1024       // cap on a request body
	connDeadline  = 5 * time.Minute // whole request/response must finish within this
	defaultCmdTTL = 60              // seconds, when a manifest sets no timeout
)

// Agent serves the vetted command set over a unix socket. It is safe to run as
// root: it only ever executes trusted scripts (see command.go) with validated,
// non-injectable parameters, and it can restrict which peer uid may connect.
type Agent struct {
	SocketPath string
	TrustedUID uint32 // required owner of command scripts (0 = root)
	AllowUID   int    // only accept connections from this peer uid; <0 = any (rely on socket perms)
	SocketGID  int    // chgrp the socket to this gid (mode 0660); <0 = leave as created
	commands   map[string]*Command
	log        *slog.Logger
}

// New loads the command registry from dir and builds an Agent. A bad manifest or a
// missing script is a startup error (fail-fast).
func New(socketPath, cmdDir string, trustedUID uint32, allowUID, socketGID int, log *slog.Logger) (*Agent, error) {
	cmds, err := LoadCommands(cmdDir)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	names := make([]string, 0, len(cmds))
	for n := range cmds {
		names = append(names, n)
	}
	sort.Strings(names)
	log.Info("commands loaded", "count", len(cmds), "commands", names)
	return &Agent{
		SocketPath: socketPath, TrustedUID: trustedUID,
		AllowUID: allowUID, SocketGID: socketGID, commands: cmds, log: log,
	}, nil
}

// Serve listens on the unix socket until ctx is cancelled.
func (a *Agent) Serve(ctx context.Context) error {
	_ = os.Remove(a.SocketPath) // clear a stale socket
	ln, err := net.Listen("unix", a.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.SocketPath, err)
	}
	defer ln.Close()
	// Restrict the socket: owner+group only (0660); optionally group-owned so only
	// the hosting extension's group can connect.
	if err := os.Chmod(a.SocketPath, 0o660); err != nil {
		return err
	}
	if a.SocketGID >= 0 {
		if err := os.Chown(a.SocketPath, 0, a.SocketGID); err != nil {
			return err
		}
	}
	a.log.Info("agent listening", "socket", a.SocketPath, "allow_uid", a.AllowUID, "trusted_uid", a.TrustedUID)

	go func() { <-ctx.Done(); ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go a.handle(conn)
	}
}

func (a *Agent) reply(conn net.Conn, resp Response) {
	_ = json.NewEncoder(conn).Encode(resp)
}

func (a *Agent) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(connDeadline))

	uid, gid, haveCreds := peerCreds(conn)
	if a.AllowUID >= 0 && (!haveCreds || int(uid) != a.AllowUID) {
		a.log.Warn("connection denied", "peer_uid", uid, "reason", "uid not allowed")
		a.reply(conn, Response{Error: "unauthorized"})
		return
	}

	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&req); err != nil {
		a.reply(conn, Response{Error: "malformed request"})
		return
	}
	cmd, ok := a.commands[req.Cmd]
	if !ok {
		a.log.Warn("unknown command", "cmd", req.Cmd, "peer_uid", uid)
		a.reply(conn, Response{Error: "unknown command"})
		return
	}

	ttl := cmd.Manifest.TimeoutSeconds
	if ttl <= 0 {
		ttl = defaultCmdTTL
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(ttl)*time.Second)
	defer cancel()

	res, err := cmd.run(ctx, req.Params, a.TrustedUID)
	// Audit: log parameter KEYS only, never values (they may carry secrets).
	if err != nil {
		a.log.Warn("command rejected/failed", "cmd", req.Cmd, "peer_uid", uid, "peer_gid", gid,
			"params", paramKeys(req.Params), "err", err.Error())
		a.reply(conn, Response{Error: err.Error()})
		return
	}
	a.log.Info("command executed", "cmd", req.Cmd, "peer_uid", uid, "peer_gid", gid,
		"params", paramKeys(req.Params), "exit", res.Code)
	a.reply(conn, Response{OK: res.Code == 0, Code: res.Code, Stdout: res.Stdout, Stderr: res.Stderr})
}

func paramKeys(p map[string]string) []string {
	ks := make([]string, 0, len(p))
	for k := range p {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
