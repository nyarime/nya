package main

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	certPEM, keyPEM, err := generateSelfSignedCert("nya.naixi.net")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no cert pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "nya.naixi.net" {
		t.Fatalf("cn=%q", cert.Subject.CommonName)
	}
	if len(keyPEM) == 0 {
		t.Fatal("empty key")
	}
}

func TestPrepareSendTLSAuto(t *testing.T) {
	cfg, err := prepareSendTLS(true, "", "", "nya.naixi.net")
	if err != nil {
		t.Fatal(err)
	}
	defer cfg.close()
	if !cfg.enabled || cfg.certFile == "" || cfg.keyFile == "" {
		t.Fatalf("cfg=%+v", cfg)
	}
}
