package version

import (
	"strings"
	"testing"
)

func TestVersionSet(t *testing.T) {
	if strings.TrimSpace(Version) == "" {
		t.Fatal("version.Version non deve essere vuota")
	}
}
