package handlers

import (
	"strings"
	"testing"
)

func TestRichHTMLRendersAndSanitizes(t *testing.T) {
	out := string(richHTML("**Grassetto** e [link](http://esempio.test)\n\n<script>alert(1)</script><img src=x onerror=alert(2)><b onclick=\"x\">y</b>"))
	// reso:
	if !strings.Contains(out, "<strong>Grassetto</strong>") {
		t.Fatalf("markdown bold non reso: %q", out)
	}
	if !strings.Contains(out, `href="http://esempio.test"`) {
		t.Fatalf("link non reso: %q", out)
	}
	// sanitizzato:
	for _, bad := range []string{"<script", "onerror", "onclick", "alert("} {
		if strings.Contains(out, bad) {
			t.Fatalf("XSS non sanitizzato (%q presente): %q", bad, out)
		}
	}
}

func TestRichHTMLEmpty(t *testing.T) {
	if richHTML("") != "" {
		t.Fatal("vuoto deve restare vuoto")
	}
}
