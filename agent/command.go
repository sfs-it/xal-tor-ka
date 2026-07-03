// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package agent is the Xal-Tor-Ka hosting agent: a small, privileged host daemon
// that runs a FIXED, vetted set of hardened commands on behalf of the (internal,
// unprivileged) hosting extension. It never runs arbitrary input:
//
//   - Each command is a script placed by root in the commands dir, owned root:root
//     (or a configured trusted owner) and NOT group/world-writable. Adding a
//     capability = dropping a vetted script + its manifest; no agent recompile,
//     and the extension can never add or alter a command (that needs root).
//   - Parameters are declared in the command's manifest with an allow-list
//     (regexp / enum). Unknown or non-matching params are rejected.
//   - Validated params reach the script only as environment variables
//     (XTK_P_<NAME>); the agent execs the script by path, never via a shell — so
//     there is no command/argument injection surface.
//
// See DRAFT-hosting-extension.md.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ParamSpec is the allow-list for one command parameter.
type ParamSpec struct {
	Required bool     `json:"required"`
	Pattern  string   `json:"pattern,omitempty"` // the value must FULLY match this regexp
	Enum     []string `json:"enum,omitempty"`    // or be one of these exact values
	re       *regexp.Regexp
}

// Manifest is the vetted contract of a command (<name>.json next to <name>.sh).
type Manifest struct {
	Description    string               `json:"description"`
	TimeoutSeconds int                  `json:"timeout_seconds"`
	Params         map[string]ParamSpec `json:"params"`
}

// Command binds a manifest to its executable script.
type Command struct {
	Name     string
	Script   string
	Manifest Manifest
}

// LoadCommands scans dir for <name>.json manifests, each requiring a matching
// executable <name>.sh, and compiles the parameter patterns. A malformed manifest
// or a missing script is a hard error (fail-fast on startup).
func LoadCommands(dir string) (map[string]*Command, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read commands dir: %w", err)
	}
	cmds := map[string]*Command{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m Manifest
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("manifest %s: %w", e.Name(), err)
		}
		for pn, ps := range m.Params {
			if ps.Pattern != "" {
				re, err := regexp.Compile("^(?:" + ps.Pattern + ")$")
				if err != nil {
					return nil, fmt.Errorf("manifest %s param %s: bad pattern: %w", e.Name(), pn, err)
				}
				ps.re = re
				m.Params[pn] = ps
			}
		}
		script := filepath.Join(dir, name+".sh")
		if _, err := os.Stat(script); err != nil {
			return nil, fmt.Errorf("command %s: missing script %s", name, script)
		}
		cmds[name] = &Command{Name: name, Script: script, Manifest: m}
	}
	return cmds, nil
}

// validate checks the request params against the manifest allow-list: every param
// must be declared; required ones must be present; values must match pattern/enum.
func (c *Command) validate(params map[string]string) error {
	for k := range params {
		if _, ok := c.Manifest.Params[k]; !ok {
			return fmt.Errorf("unknown parameter %q", k)
		}
	}
	// deterministic order for stable error messages
	names := make([]string, 0, len(c.Manifest.Params))
	for n := range c.Manifest.Params {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		spec := c.Manifest.Params[n]
		v, present := params[n]
		if !present {
			if spec.Required {
				return fmt.Errorf("missing required parameter %q", n)
			}
			continue
		}
		if len(spec.Enum) > 0 {
			ok := false
			for _, e := range spec.Enum {
				if v == e {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("parameter %q not in allowed set", n)
			}
		}
		if spec.re != nil && !spec.re.MatchString(v) {
			return fmt.Errorf("parameter %q does not match the allowed pattern", n)
		}
	}
	return nil
}

// checkScriptTrust refuses a script that is not a regular file owned by the
// trusted uid and not writable by group/other — so a compromised unprivileged
// account cannot plant or alter a command the agent will run.
func checkScriptTrust(path string, trustedUID uint32) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("group/other-writable script refused (%o)", fi.Mode().Perm())
	}
	if uid, ok := fileUID(fi); ok && uid != trustedUID {
		return fmt.Errorf("script not owned by trusted uid %d (owner %d)", trustedUID, uid)
	}
	return nil
}

// Result is the outcome of running a command.
type Result struct {
	Code   int
	Stdout string
	Stderr string
}

// run validates the params, verifies the script trust, and executes it with the
// params passed ONLY as environment (XTK_P_<NAME>), never via a shell.
func (c *Command) run(ctx context.Context, params map[string]string, trustedUID uint32) (Result, error) {
	if err := c.validate(params); err != nil {
		return Result{}, err
	}
	if err := checkScriptTrust(c.Script, trustedUID); err != nil {
		return Result{}, fmt.Errorf("untrusted command script: %w", err)
	}
	cmd := exec.CommandContext(ctx, c.Script)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"XTK_AGENT=1",
	}
	for k, v := range params {
		cmd.Env = append(cmd.Env, "XTK_P_"+strings.ToUpper(k)+"="+v)
	}
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if ee, ok := err.(*exec.ExitError); ok {
		res.Code = ee.ExitCode()
		return res, nil // non-zero exit is a result, not a transport error
	}
	if err != nil {
		return res, err
	}
	return res, nil
}
