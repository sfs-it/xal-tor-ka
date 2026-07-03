// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package agent

import (
	"net"
	"syscall"
)

// peerCreds returns the uid/gid of the process on the other end of a unix socket
// connection (Linux SO_PEERCRED). ok=false if unavailable.
func peerCreds(conn net.Conn) (uid, gid uint32, ok bool) {
	uc, isUnix := conn.(*net.UnixConn)
	if !isUnix {
		return 0, 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, false
	}
	var cred *syscall.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || serr != nil || cred == nil {
		return 0, 0, false
	}
	return cred.Uid, cred.Gid, true
}
