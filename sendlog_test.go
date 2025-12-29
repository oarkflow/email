package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestCountSuccessesFromReader(t *testing.T) {
	now := time.Now().UTC()
	// create two entries: one matching provider and domain, one not
	entries := []string{
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"smtp","success":true,"recipients":["user@example.com"]}`,
	}
	r := strings.NewReader(strings.Join(entries, "\n") + "\n")
	since := now.Add(-1 * time.Hour)
	cnt, err := countSuccessesFromReader(r, []string{"sendgrid"}, since, []string{"gmail.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 got %d", cnt)
	}
}

func TestResolveProviders_SkipRouteWhenLimitExceeded(t *testing.T) {
	defer withTempSendLog(t)()
	// write a single successful send for sendgrid->gmail to exhaust hourly_limit=1
	now := time.Now().UTC()
	entry := `{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}` + "\n"
	if err := os.WriteFile(sendLogFile, []byte(entry), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid"}, HourlyLimit: 1},
		},
	}
	got := resolveProviders(cfg)
	exp := []string{"smtp"} // route exhausted so fallback to smtp
	if len(got) != len(exp) {
		t.Fatalf("expected %v got %v", exp, got)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Fatalf("expected %v got %v", exp, got)
		}
	}
}

func TestSendEmail_DryRun(t *testing.T) {
	cfg := &EmailConfig{
		From:      "Acme <noreply@acme.example>",
		To:        []string{"user@example.com"},
		Subject:   "Dry Run",
		Transport: "smtp",
		Host:      "localhost",
		Port:      1025,
		Provider:  "smtp",
		DryRun:    true,
	}
	if err := sendEmail(cfg, nil); err != nil {
		t.Fatalf("expected no error for dry-run, got %v", err)
	}
}
