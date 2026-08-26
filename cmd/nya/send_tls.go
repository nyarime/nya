package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

type sendTLSConfig struct {
	enabled  bool
	certFile string
	keyFile  string
	cleanup  func()
}

func (c *sendTLSConfig) close() {
	if c.cleanup != nil {
		c.cleanup()
		c.cleanup = nil
	}
}

func prepareSendTLS(tlsFlag bool, certPath, keyPath, host string) (*sendTLSConfig, error) {
	cfg := &sendTLSConfig{}
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("send: -tls-cert and -tls-key must be used together")
		}
		cfg.enabled = true
		cfg.certFile = certPath
		cfg.keyFile = keyPath
		return cfg, nil
	}
	if !tlsFlag {
		return cfg, nil
	}
	if host == "" {
		host = "nya.naixi.net"
	}
	certPEM, keyPEM, err := generateSelfSignedCert(host)
	if err != nil {
		return nil, err
	}
	certFile, err := os.CreateTemp("", "nya-send-*.crt")
	if err != nil {
		return nil, err
	}
	keyFile, err := os.CreateTemp("", "nya-send-*.key")
	if err != nil {
		_ = certFile.Close()
		_ = os.Remove(certFile.Name())
		return nil, err
	}
	if _, err := certFile.Write(certPEM); err != nil {
		_ = certFile.Close()
		_ = keyFile.Close()
		return nil, err
	}
	if _, err := keyFile.Write(keyPEM); err != nil {
		_ = certFile.Close()
		_ = keyFile.Close()
		return nil, err
	}
	_ = certFile.Close()
	_ = keyFile.Close()
	cfg.enabled = true
	cfg.certFile = certFile.Name()
	cfg.keyFile = keyFile.Name()
	cfg.cleanup = func() {
		_ = os.Remove(cfg.certFile)
		_ = os.Remove(cfg.keyFile)
	}
	return cfg, nil
}

func generateSelfSignedCert(primaryHost string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	hosts := uniqueHosts(primaryHost, "localhost", "127.0.0.1")
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: primaryHost,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{},
		IPAddresses:           []net.IP{},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			continue
		}
		tmpl.DNSNames = append(tmpl.DNSNames, h)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func uniqueHosts(hosts ...string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func tlsListen(ln net.Listener, certFile, keyFile string) (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}), nil
}
