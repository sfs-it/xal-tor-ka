// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package config

import "strings"

// expandEnv expands ${VAR} and ${VAR:-default} references using lookup
// (typically os.LookupEnv). A bare $VAR is left untouched on purpose, to avoid
// mangling values such as upstream URLs; only the ${...} form is processed.
func expandEnv(s string, lookup func(string) (string, bool)) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			if end := strings.IndexByte(s[i+2:], '}'); end >= 0 {
				expr := s[i+2 : i+2+end]
				b.WriteString(resolveExpr(expr, lookup))
				i = i + 2 + end + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// resolveExpr resolves "NAME" or "NAME:-default". An unset or empty variable
// falls back to the default when provided, else to the empty string.
func resolveExpr(expr string, lookup func(string) (string, bool)) string {
	name, def := expr, ""
	hasDef := false
	if idx := strings.Index(expr, ":-"); idx >= 0 {
		name, def, hasDef = expr[:idx], expr[idx+2:], true
	}
	if v, ok := lookup(name); ok && v != "" {
		return v
	}
	if hasDef {
		return def
	}
	return ""
}
