// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package vpnmgr

import "context"

// wgDriver is the WireGuard engine. In F1 it is a SCAFFOLD: Status() reports the
// data-plane is not installed yet and every mutating method returns ErrNotImplemented.
// F2 wires each method to a vetted xtk-vpn-agent command (wg_up/down, wg_peer_add,
// wg_syncconf, nft_apply). No host access happens in this code directly: every
// privileged operation goes through the AgentCaller → the root-vetted agent → the
// NET_ADMIN data-plane container.
type wgDriver struct {
	agent AgentCaller
}

func newWG(a AgentCaller) *wgDriver { return &wgDriver{agent: a} }

func (d *wgDriver) Name() string { return "wireguard" }

// Status is read-only. F1: inert — the data-plane container (xtk-vpn, NET_ADMIN/wg0)
// arrives in F2, so there is nothing live to report yet.
func (d *wgDriver) Status(ctx context.Context) (Status, error) {
	return Status{
		Driver: "wireguard",
		Ready:  false,
		Reason: "data-plane non installato (fase F2) — modulo in scaffold F1, inerte",
	}, nil
}

func (d *wgDriver) Up(ctx context.Context, cfg Config) error { return ErrNotImplemented }
func (d *wgDriver) Down(ctx context.Context) error           { return ErrNotImplemented }
func (d *wgDriver) SyncConf(ctx context.Context, cfg Config, m Matrix) error {
	return ErrNotImplemented
}

func (d *wgDriver) GenPeer(ctx context.Context, req PeerRequest) (PeerResult, error) {
	return PeerResult{}, ErrNotImplemented
}

func (d *wgDriver) RenderClientConfig(p Peer) ([]byte, error) { return nil, ErrNotImplemented }
