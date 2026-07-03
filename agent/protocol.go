// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package agent

// Request is one command invocation over the socket (one request per connection).
// Cmd must be a registered command name; Params are validated against its manifest.
type Request struct {
	Cmd    string            `json:"cmd"`
	Params map[string]string `json:"params,omitempty"`
}

// Response is the agent's reply. OK is true only on a zero exit code; Error is set
// for transport/validation failures (before the script runs).
type Response struct {
	OK     bool   `json:"ok"`
	Code   int    `json:"code"`
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Error  string `json:"error,omitempty"`
}
