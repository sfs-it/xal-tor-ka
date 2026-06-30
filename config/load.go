// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package config loads and validates the three configuration files of
// Xal-Tor-Ka (config.json, secrets.json, users.json). See BLUEPRINT.md §7.
// Decoding is strict (DisallowUnknownFields) and fails fast with the offending
// file/field (MYRULES Go §5).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xaltorka/models"
)

// Bundle groups the decoded configuration files.
type Bundle struct {
	Config   models.Config
	Secrets  models.Secrets
	Users    models.Users
	Services models.Services
}

// Load reads, env-expands, strictly decodes and validates the configuration
// from dir. config.json drives the paths of the other two files
// (secrets_file / users_file), resolved relative to dir when not absolute.
func Load(dir string) (*Bundle, error) {
	var b Bundle

	if err := decodeFile(filepath.Join(dir, "config.json"), &b.Config); err != nil {
		return nil, err
	}

	secretsPath := b.Config.SecretsFile
	if secretsPath == "" {
		secretsPath = "secrets.json"
	}
	if err := decodeFile(resolve(dir, secretsPath), &b.Secrets); err != nil {
		return nil, err
	}

	usersPath := b.Config.UsersFile
	if usersPath == "" {
		usersPath = "users.json"
	}
	if err := decodeFile(resolve(dir, usersPath), &b.Users); err != nil {
		return nil, err
	}

	// services.json is optional: absent => no extra services.
	svc, err := LoadServices(resolve(dir, servicesFileName(&b.Config)))
	if err != nil {
		return nil, err
	}
	b.Services = svc

	if err := Validate(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

func servicesFileName(c *models.Config) string {
	if c.ServicesFile == "" {
		return "services.json"
	}
	return c.ServicesFile
}

// resolve makes p absolute relative to dir unless it already is.
func resolve(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

// decodeFile reads p, expands ${VAR} references from the environment, then
// strictly decodes the JSON into v (unknown fields are an error).
func decodeFile(p string, v any) error {
	raw, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read config file %s: %w", p, err)
	}
	expanded := expandEnv(string(raw), os.LookupEnv)

	dec := json.NewDecoder(strings.NewReader(expanded))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parse config file %s: %w", p, err)
	}
	return nil
}
