// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package certmgr

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

// LetsEncryptProd is the default ACME directory (Let's Encrypt production).
const LetsEncryptProd = "https://acme-v02.api.letsencrypt.org/directory"

const acmeAcctKeyFile = "acme_account.key"

func (m *Manager) directoryURL() string {
	if m.DirectoryURL != "" {
		return m.DirectoryURL
	}
	return LetsEncryptProd
}

// acmeClient loads or creates the ACME account key and returns a registered
// client. The account key is persisted (0600) so the same account is reused.
func (m *Manager) acmeClient(ctx context.Context) (*acme.Client, error) {
	keyPath := filepath.Join(m.Dir, acmeAcctKeyFile)
	var key *ecdsa.PrivateKey
	if pemBytes, err := os.ReadFile(keyPath); err == nil {
		if kb, _ := pem.Decode(pemBytes); kb != nil {
			key, _ = x509.ParseECPrivateKey(kb.Bytes)
		}
	}
	newAccount := key == nil
	if newAccount {
		var err error
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			return nil, err
		}
		keyPEM, err := marshalKey(key)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(m.Dir, 0o755); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
			return nil, err
		}
	}
	client := &acme.Client{Key: key, DirectoryURL: m.directoryURL()}
	if newAccount {
		acct := &acme.Account{}
		if m.Email != "" {
			acct.Contact = []string{"mailto:" + m.Email}
		}
		if _, err := client.Register(ctx, acct, acme.AcceptTOS); err != nil {
			return nil, fmt.Errorf("acme register: %w", err)
		}
	}
	return client, nil
}

// IssueACME obtains (or renews) a certificate for host via the HTTP-01 challenge
// and writes it to the shared cert dir. Requires the host to resolve publicly to
// the gateway with port 80 reachable (NGINX proxies /.well-known/acme-challenge/
// to this service).
func (m *Manager) IssueACME(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	client, err := m.acmeClient(ctx)
	if err != nil {
		return err
	}
	order, err := client.AuthorizeOrder(ctx, []acme.AuthzID{{Type: "dns", Value: host}})
	if err != nil {
		return fmt.Errorf("authorize order: %w", err)
	}
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return err
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "http-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return fmt.Errorf("no http-01 challenge offered for %s", host)
		}
		resp, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return err
		}
		m.setChallenge(chal.Token, resp)
		if _, err := client.Accept(ctx, chal); err != nil {
			m.delChallenge(chal.Token)
			return fmt.Errorf("accept challenge: %w", err)
		}
		if _, err := client.WaitAuthorization(ctx, authzURL); err != nil {
			m.delChallenge(chal.Token)
			return fmt.Errorf("wait authorization: %w", err)
		}
		m.delChallenge(chal.Token)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: host},
		DNSNames: []string{host},
	}, key)
	if err != nil {
		return err
	}
	der, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return fmt.Errorf("finalize order: %w", err)
	}
	var chain []byte
	for _, b := range der {
		chain = append(chain, pemBlock("CERTIFICATE", b)...)
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return err
	}
	slog.Info("acme certificate issued", "host", host)
	return m.writePair(host, chain, keyPEM)
}

func (m *Manager) setChallenge(token, keyAuth string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.challenges == nil {
		m.challenges = map[string]string{}
	}
	m.challenges[token] = keyAuth
}

func (m *Manager) delChallenge(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.challenges, token)
}

// HTTP01Handler serves the ACME HTTP-01 key authorizations. Mount it at
// /.well-known/acme-challenge/ (NGINX proxies that path here on port 80).
func (m *Manager) HTTP01Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
		m.mu.Lock()
		ka := m.challenges[token]
		m.mu.Unlock()
		if ka == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(ka))
	})
}

// StartRenewal periodically renews ACME certs that are within `within` of expiry.
// hosts() supplies the current served hosts; only ACME-sourced certs are renewed.
// The goroutine exits on ctx cancellation (mirrors the health checker).
func (m *Manager) StartRenewal(ctx context.Context, hosts func() []string, within time.Duration) {
	t := time.NewTicker(12 * time.Hour)
	defer t.Stop()
	m.renewDue(ctx, hosts(), within)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.renewDue(ctx, hosts(), within)
		}
	}
}

func (m *Manager) renewDue(ctx context.Context, hosts []string, within time.Duration) {
	for _, h := range hosts {
		in := m.Info(h)
		if in.Source != SourceACME || time.Until(in.NotAfter) >= within {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if err := m.IssueACME(c, h); err != nil {
			slog.Warn("acme renewal failed", "host", h, "err", err)
		}
		cancel()
	}
}
