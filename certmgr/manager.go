// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

// Package certs manages TLS certificates for the hosts served by the gateway:
// an internal self-signed CA (for LAN/dev hosts without public DNS) and ACME /
// Let's Encrypt via the HTTP-01 challenge (for public hosts). Certificates are
// written to a directory shared with the NGINX container, which references them
// in the generated vhosts; the Go service writes + triggers a reload. See
// DRAFT-tls-ui.md and BLUEPRINT §TLS.
package certmgr

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// caCommonName marks certificates issued by our internal CA, so List() can tell
// self-signed certs apart from ACME ones by their issuer.
const caCommonName = "Xal-Tor-Ka internal CA"

// Source classifies where a host certificate came from.
type Source string

const (
	SourceNone       Source = ""
	SourceSelfSigned Source = "selfsigned"
	SourceACME       Source = "acme"
)

// Info is the certificate status for one host (for the admin TLS page).
type Info struct {
	Host     string
	Source   Source
	NotAfter time.Time
	Valid    bool // present and not expired
}

// Manager issues and stores certificates. It is safe for concurrent use.
type Manager struct {
	// Dir is where the Go service reads/writes cert files (e.g. <configdir>/certs).
	Dir string
	// NginxDir is the same directory as seen from the NGINX container
	// (e.g. /etc/nginx/certs); used only to render ssl_certificate paths.
	NginxDir string
	// Email is the ACME account contact (from config tls.acme.email).
	Email string
	// DirectoryURL is the ACME directory endpoint (default: Let's Encrypt prod).
	DirectoryURL string
	// Reload is invoked after a cert is written so NGINX picks it up (may be nil).
	Reload func() error
}

func (m *Manager) certPath(host string) string { return filepath.Join(m.Dir, host+".crt") }
func (m *Manager) keyPath(host string) string  { return filepath.Join(m.Dir, host+".key") }
func (m *Manager) caCertPath() string          { return filepath.Join(m.Dir, "ca.crt") }
func (m *Manager) caKeyPath() string           { return filepath.Join(m.Dir, "ca.key") }

// NginxCertPath / NginxKeyPath return the cert paths as NGINX sees them.
func (m *Manager) NginxCertPath(host string) string { return filepath.Join(m.NginxDir, host+".crt") }
func (m *Manager) NginxKeyPath(host string) string  { return filepath.Join(m.NginxDir, host+".key") }

// HasCert reports whether a usable cert+key pair exists for host.
func (m *Manager) HasCert(host string) bool {
	if m == nil || m.Dir == "" || host == "" {
		return false
	}
	if _, err := os.Stat(m.certPath(host)); err != nil {
		return false
	}
	_, err := os.Stat(m.keyPath(host))
	return err == nil
}

// CAExists reports whether the internal CA has been generated.
func (m *Manager) CAExists() bool {
	if m == nil || m.Dir == "" {
		return false
	}
	_, err := os.Stat(m.caCertPath())
	return err == nil
}

// CACertPEM returns the internal CA certificate for download (or an error if the
// CA has not been generated yet).
func (m *Manager) CACertPEM() ([]byte, error) {
	return os.ReadFile(m.caCertPath())
}

// Info returns the certificate status for a host.
func (m *Manager) Info(host string) Info {
	in := Info{Host: host, Source: SourceNone}
	der, err := os.ReadFile(m.certPath(host))
	if err != nil {
		return in
	}
	block, _ := pem.Decode(der)
	if block == nil {
		return in
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return in
	}
	in.NotAfter = c.NotAfter
	in.Valid = time.Now().Before(c.NotAfter)
	if c.Issuer.CommonName == caCommonName {
		in.Source = SourceSelfSigned
	} else {
		in.Source = SourceACME
	}
	return in
}

// List returns the cert status for each host, in the given order.
func (m *Manager) List(hosts []string) []Info {
	out := make([]Info, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, m.Info(h))
	}
	return out
}

// writePair writes a cert (PEM chain) and its private key atomically, the key
// with 0600 perms, then triggers the reload hook.
func (m *Manager) writePair(host string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(m.certPath(host), certPEM, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(m.keyPath(host), keyPEM, 0o600); err != nil {
		return err
	}
	if m.Reload != nil {
		return m.Reload()
	}
	return nil
}

// Delete removes the cert+key for a host and reloads.
func (m *Manager) Delete(host string) error {
	_ = os.Remove(m.certPath(host))
	_ = os.Remove(m.keyPath(host))
	if m.Reload != nil {
		return m.Reload()
	}
	return nil
}

// ---- internal CA + self-signed issuance -----------------------------------

// ensureCA loads or generates the internal CA (cert + key).
func (m *Manager) ensureCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	if certPEM, err := os.ReadFile(m.caCertPath()); err == nil {
		keyPEM, err := os.ReadFile(m.caKeyPath())
		if err == nil {
			ca, key, err := parseCertKey(certPEM, keyPEM)
			if err == nil {
				return ca, key, nil
			}
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: caCommonName, Organization: []string{"Xal-Tor-Ka"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(m.caCertPath(), pemBlock("CERTIFICATE", der), 0o644); err != nil {
		return nil, nil, err
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(m.caKeyPath(), keyPEM, 0o600); err != nil {
		return nil, nil, err
	}
	ca, err := x509.ParseCertificate(der)
	return ca, key, err
}

// IssueSelfSigned issues a host certificate signed by the internal CA.
func (m *Manager) IssueSelfSigned(host string, extra ...string) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	ca, caKey, err := m.ensureCA()
	if err != nil {
		return fmt.Errorf("internal CA: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, 825),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     append([]string{host}, extra...),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return err
	}
	// Serve leaf + CA so a client that trusts the CA validates the chain.
	chain := append(pemBlock("CERTIFICATE", der), pemBlock("CERTIFICATE", ca.Raw)...)
	return m.writePair(host, chain, keyPEM)
}

// ---- small crypto/pem helpers ---------------------------------------------

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	return n
}

func pemBlock(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func marshalKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pemBlock("EC PRIVATE KEY", der), nil
}

func parseCertKey(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, fmt.Errorf("invalid PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
