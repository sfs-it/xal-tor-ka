// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package i18n provides UI localization: a message catalog per language
// (embedded JSON files under locales/), language negotiation from a cookie or
// the Accept-Language header, and a T(lang, key) lookup with fallback to English
// and finally to the key itself. English (en) is the base language.
package i18n

import (
	"embed"
	"encoding/json"
	"strings"
)

//go:embed locales/*.json
var localesFS embed.FS

// Default is the base/fallback language code.
const Default = "en"

// Lang is a supported UI language.
type Lang struct{ Code, Name string }

// Supported lists the offered languages in display order. A code is only usable
// if a matching locales/<code>.json exists (loaded at init).
var Supported = []Lang{
	{"en", "English"}, {"it", "Italiano"}, {"fr", "Français"}, {"es", "Español"},
	{"de", "Deutsch"}, {"ru", "Русский"}, {"pt", "Português"}, {"zh", "中文"},
	{"hi", "हिन्दी"}, {"ar", "العربية"},
}

// catalogs is lang → (key → text), populated once at init (read-only afterwards).
var catalogs = map[string]map[string]string{}

func init() {
	entries, _ := localesFS.ReadDir("locales")
	for _, e := range entries {
		b, err := localesFS.ReadFile("locales/" + e.Name())
		if err != nil {
			continue
		}
		var m map[string]string
		if json.Unmarshal(b, &m) == nil {
			catalogs[strings.TrimSuffix(e.Name(), ".json")] = m
		}
	}
}

// IsSupported reports whether a catalog exists for the language code.
func IsSupported(code string) bool { _, ok := catalogs[code]; return ok }

// T returns the translation of key for lang, falling back to English, then key.
func T(lang, key string) string {
	if m, ok := catalogs[lang]; ok {
		if v, ok := m[key]; ok && v != "" {
			return v
		}
	}
	if m, ok := catalogs[Default]; ok {
		if v, ok := m[key]; ok && v != "" {
			return v
		}
	}
	return key
}

// IsRTL reports whether a language is written right-to-left (for dir="rtl").
func IsRTL(lang string) bool { return lang == "ar" }

// Match picks the best supported language: a valid cookie wins, otherwise the
// first supported tag in the Accept-Language header, otherwise Default.
func Match(cookie, acceptLang string) string {
	if cookie != "" && IsSupported(cookie) {
		return cookie
	}
	for _, part := range strings.Split(acceptLang, ",") {
		tag := strings.TrimSpace(part)
		if i := strings.IndexByte(tag, ';'); i >= 0 {
			tag = tag[:i]
		}
		tag = strings.ToLower(strings.TrimSpace(tag))
		if i := strings.IndexByte(tag, '-'); i >= 0 {
			tag = tag[:i] // "it-IT" → "it"
		}
		if IsSupported(tag) {
			return tag
		}
	}
	return Default
}
