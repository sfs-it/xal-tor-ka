// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package version

import (
	"strings"
	"testing"
)

func TestVersionSet(t *testing.T) {
	if strings.TrimSpace(Version) == "" {
		t.Fatal("version.Version must not be empty")
	}
}
