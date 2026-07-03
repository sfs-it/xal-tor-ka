// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Command xtk-agent is the Xal-Tor-Ka hosting agent: a privileged host daemon that
// serves a FIXED, vetted set of hardened commands (scripts placed by root) over a
// unix socket, on behalf of the internal hosting extension. It never runs arbitrary
// input. See package agent and DRAFT-hosting-extension.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"xaltorka/agent"
)

func main() {
	socket := flag.String("socket", "/run/xtk-agent.sock", "unix socket to listen on")
	cmdDir := flag.String("commands", "/usr/local/lib/xtk-agent/commands", "directory of vetted command scripts + manifests")
	trustedUID := flag.Uint("trusted-uid", 0, "required owner uid of every command script (0 = root)")
	allowUID := flag.Int("allow-uid", -1, "only accept connections from this peer uid (-1 = any; rely on socket perms)")
	socketGID := flag.Int("socket-gid", -1, "chgrp the socket to this gid, mode 0660 (-1 = leave as created)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	a, err := agent.New(*socket, *cmdDir, uint32(*trustedUID), *allowUID, *socketGID, log)
	if err != nil {
		log.Error("agent startup failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := a.Serve(ctx); err != nil {
		log.Error("agent serve failed", "err", err)
		os.Exit(1)
	}
	log.Info("agent stopped")
}
