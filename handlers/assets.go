// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import "embed"

// assetsFS embeds the static assets (CSS) into the binary, so the UI stays
// self-contained in the minimal container (no external file mounts, no CDN).
//
//go:embed assets/admin.css assets/admin.js
var assetsFS embed.FS
