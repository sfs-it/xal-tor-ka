// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package vpnmgr is the VPN module's engine abstraction and config model. It is
// deliberately NOT named "vpn": a runtime data dir named vpn/ could be git/docker
// ignored and shadow a Go package of the same name (the certs→certmgr trap,
// CHANGELOG.md). Runtime state lives under data/vpn/ (already ignored); this package
// carries only code. See ext/vpn/DESIGN.md for the full model (rev 0.5).
package vpnmgr

// Config mirrors vpn.json — the module's OWN config, owned by the extension and kept
// OUT of the core config.json (which is strict-decode: a new key there would need a Go
// struct field + a core rebuild, and would couple core↔module). Static fields (driver,
// role, ports, subnets) change rarely; the Machines/Users matrix is edited hot.
type Config struct {
	Enabled     bool      `json:"enabled"`
	Driver      string    `json:"driver"`       // "wireguard" (v1); "openvpn"/"ipsec" planned
	Role        string    `json:"role"`         // "hub" | "spoke"
	ListenPort  int       `json:"listen_port"`  // WireGuard UDP port (hub side)
	VPNSubnet   string    `json:"vpn_subnet"`   // tunnel subnet, e.g. 10.8.0.0/24
	DockerNet   string    `json:"docker_net"`   // data-plane docker network, e.g. 172.31.7.0/24
	PrivateNets []string  `json:"private_nets"` // co-located private subnets reachable via route (A)
	Machines    []Machine `json:"vpn_machines"`
	Users       []User    `json:"vpn_users"`
}

// Machine is a target the hub can reach. Reach picks the via: "route" (A, co-located,
// zero-agent — the hub routes to Target) or "tunnel" (B, remote spoke — reachable at Addr).
type Machine struct {
	ID       string   `json:"id"`
	Reach    string   `json:"reach"`              // "route" | "tunnel"
	Target   string   `json:"target,omitempty"`   // A: private IP the hub routes to
	Services []string `json:"services,omitempty"` // A: exposed ports, e.g. "10000/tcp"
	Addr     string   `json:"addr,omitempty"`     // B: tunnel IP, e.g. 10.8.0.12
	Harden   *Harden  `json:"harden,omitempty"`   // optional "behind-hub" hardening (F5)
}

// Harden marks a machine for the "solo-dietro-hub" lockdown (F5); Provider names the
// transparent-firewall layer (e.g. "hetzner") the client CLI can also close.
type Harden struct {
	Provider string `json:"provider,omitempty"`
}

// User is a gate user (users.json) with a WireGuard peer and a row of the access
// matrix: CanReach lists the machine IDs / tunnel addrs this user may forward to. The
// hub enforces it (nftables forward-filter), never the client.
type User struct {
	User     string   `json:"user"`
	PeerAddr string   `json:"peer_addr"`        // tunnel IP, e.g. 10.8.0.2
	PubKey   string   `json:"pubkey,omitempty"` // WG public key (the private key stays on the peer)
	CanReach []string `json:"can_reach"`
}

// Matrix is the (machines, users) pair the routing + forward-filter is generated from.
type Matrix struct {
	Machines []Machine
	Users    []User
}

// Status is the driver's read-only view. F1: a placeholder (Ready=false); F2: real,
// backed by `wg show` via the vetted agent.
type Status struct {
	Driver string     `json:"driver"`
	Ready  bool       `json:"ready"`
	Reason string     `json:"reason,omitempty"`
	Peers  []PeerStat `json:"peers,omitempty"`
}

// PeerStat is one connected peer's live state (F2+).
type PeerStat struct {
	User          string `json:"user"`
	Addr          string `json:"addr"`
	LastHandshake string `json:"last_handshake,omitempty"`
}

// PeerRequest / PeerResult drive onboarding. INVARIANT: the private key is generated
// ON the peer machine and is NEVER transmitted; the hub only ever receives the public
// key and hands back the assigned tunnel address + how to reach the hub.
type PeerRequest struct {
	User   string
	PubKey string // supplied by the peer; its private key never leaves the machine
}

// PeerResult is what the hub returns to a joining peer.
type PeerResult struct {
	Addr      string // tunnel IP assigned by the hub
	HubPubKey string
	Endpoint  string // host:port of the hub
}

// Peer is what RenderClientConfig needs to emit a client wg0.conf.
type Peer struct {
	Addr       string
	HubPubKey  string
	Endpoint   string
	AllowedIPs []string
}
