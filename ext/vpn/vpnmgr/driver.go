// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package vpnmgr

import (
	"context"
	"errors"
	"fmt"

	"xaltorka/agent"
)

// ErrNotImplemented marks driver methods not yet wired in the current phase. In F1 the
// WireGuard driver is a scaffold; F2 implements the mutating methods over the vetted
// xtk-vpn-agent.
var ErrNotImplemented = errors.New("vpn: not implemented in this phase")

// AgentCaller is the minimal transport the drivers use to run vetted PRIVILEGED
// commands on the host, over the xtk-vpn-agent unix socket. Keeping it an interface
// lets vpnmgr stay transport-agnostic and unit-testable, and keeps host powers out of
// the driver code itself (the driver asks; the vetted agent does).
type AgentCaller interface {
	Call(ctx context.Context, cmd string, params map[string]string) (agent.Response, error)
}

// Driver abstracts the VPN engine. WireGuard is the v1 implementation; OpenVPN and
// IPsec are planned. Topology, the access matrix, routing and onboarding are shared
// across drivers — only the tunnel mechanics differ. See ext/vpn/DESIGN.md §3.
type Driver interface {
	Name() string
	Status(ctx context.Context) (Status, error)
	Up(ctx context.Context, cfg Config) error
	Down(ctx context.Context) error
	SyncConf(ctx context.Context, cfg Config, m Matrix) error
	GenPeer(ctx context.Context, req PeerRequest) (PeerResult, error)
	RenderClientConfig(p Peer) ([]byte, error)
}

// New selects the driver implementation from cfg.Driver. Unknown or not-yet-available
// drivers fail closed (no silent fallback to a different engine).
func New(cfg Config, a AgentCaller) (Driver, error) {
	switch cfg.Driver {
	case "wireguard", "":
		return newWG(a), nil
	case "openvpn", "ipsec":
		return nil, fmt.Errorf("vpn: driver %q planned but not yet available", cfg.Driver)
	default:
		return nil, fmt.Errorf("vpn: unknown driver %q", cfg.Driver)
	}
}
