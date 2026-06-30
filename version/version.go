// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package version holds the single source of truth for the Xal-Tor-Ka build
// version. The default tracks the active pre-release line; release builds override
// it at link time:
//
//	go build -ldflags "-X xaltorka/version.Version=beta0.2" .
package version

// Version is the current release identifier (pre-1.0 beta line: beta0.1, beta0.2, …).
var Version = "beta0.1"
