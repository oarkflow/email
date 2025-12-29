package main

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestMailHogProviderRegistered(t *testing.T) {
	p, ok := GetProvider("mailhog")
	if !ok {
		t.Fatalf("mailhog provider is not registered")
	}
	if p.Transport() != "smtp" {
		t.Fatalf("mailhog provider should be smtp transport, got %s", p.Transport())
	}
}

// requireMailHog tries to connect to localhost:1025 and skips the test if not reachable.
func requireMailHog(t *testing.T) net.Conn {
	t.Helper()
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := d.Dial("tcp", "localhost:1025")
	if err != nil {
		if os.Getenv("RUN_MAILHOG_INTEGRATION") != "1" {
			t.Skip("skipping MailHog integration test; set RUN_MAILHOG_INTEGRATION=1 to enable")
		}
		t.Fatalf("expected MailHog to be listening on localhost:1025: %v", err)
	}
	return conn
}

func TestMailHogIntegration_SMTPHandshake(t *testing.T) {
	conn := requireMailHog(t)
	defer conn.Close()
	// simple SMTP handshake: expect an initial 220 banner
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read banner from MailHog: %v", err)
	}
	banner := string(buf[:n])
	if len(banner) < 3 || banner[:3] != "220" {
		t.Fatalf("unexpected SMTP banner from MailHog: %q", banner)
	}
	// send QUIT
	_, _ = conn.Write([]byte("QUIT\r\n"))
}
