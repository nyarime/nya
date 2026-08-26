package main

import "testing"

func TestSendOriginTLS(t *testing.T) {
	if !sendOriginTLS(false, false, false, "", "") {
		t.Fatal("tunnel on => TLS by default")
	}
	if sendOriginTLS(false, true, false, "", "") {
		t.Fatal("-no-tls should disable")
	}
	if !sendOriginTLS(true, false, true, "", "") {
		t.Fatal("-tls forces on for LAN")
	}
	if !sendOriginTLS(false, true, false, "a.pem", "b.pem") {
		t.Fatal("cert/key forces TLS")
	}
	if sendOriginTLS(true, false, false, "", "") {
		t.Fatal("LAN without -tls stays HTTP")
	}
}
