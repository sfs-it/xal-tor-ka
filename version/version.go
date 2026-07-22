// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package version holds the SINGLE SOURCE OF TRUTH for the Xal-Tor-Ka build
// version. To cut a new pre-release, bump the constant below — nothing else: the
// Makefile derives it from here and the Dockerfile bakes it in without any -X
// override, so there is no version string to keep in sync anywhere else.
package version

// Version is the current release identifier (pre-1.0 beta line: beta0.1, beta0.2, …).
var Version = "beta0.12"
