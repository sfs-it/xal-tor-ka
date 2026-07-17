// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package providers

import (
	"errors"
	"testing"
)

func TestLDAPEscapeDNValue(t *testing.T) {
	cases := map[string]string{
		"mario.rossi": "mario.rossi", // normal username → unchanged (safe for UPN)
		"a,b":         `a\,b`,
		"x=y+z":       `x\=y\+z`,
		`a\b`:         `a\\b`,
		`x"y`:         `x\"y`,
	}
	for in, want := range cases {
		if got := escapeDNValue(in); got != want {
			t.Errorf("escapeDNValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLDAPTypeID(t *testing.T) {
	l := NewLDAP("corp", "ldaps://dc:636", "%s@corp.example.com", false, false)
	if l.ID() != "corp" {
		t.Errorf("ID = %q, want corp", l.ID())
	}
	if l.Type() != "ldap" {
		t.Errorf("Type = %q, want ldap", l.Type())
	}
}

func TestLDAPEmptyCredentialsRejected(t *testing.T) {
	// Empty username or password must fail *without* touching the network (an
	// empty password would be an anonymous bind on many servers).
	l := NewLDAP("corp", "ldaps://unreachable.invalid:636", "%s@corp.example.com", false, false)
	if _, err := l.Authenticate("", "secret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("empty user: got %v, want ErrInvalidCredentials", err)
	}
	if _, err := l.Authenticate("mario", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("empty pass: got %v, want ErrInvalidCredentials", err)
	}
}
