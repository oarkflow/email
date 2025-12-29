package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func withTempSendLog(t *testing.T) func() {
	tmpf, err := os.CreateTemp("", "sendlog-*.jsonl")
	if err != nil {
		t.Fatalf("cannot create temp log: %v", err)
	}
	_ = tmpf.Close()
	orig := sendLogFile
	sendLogFile = tmpf.Name()
	return func() { sendLogFile = orig; os.Remove(tmpf.Name()) }
}

func TestResolveProviders_PriorityExplicit(t *testing.T) {
	cfg := &EmailConfig{
		ProviderPriority: []string{"sendgrid", "smtp"},
		Provider:         "smtp",
	}
	got := resolveProviders(cfg)
	exp := []string{"sendgrid", "smtp"}
	if len(got) != len(exp) {
		t.Fatalf("expected %v got %v", exp, got)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Fatalf("expected %v got %v", exp, got)
		}
	}
}

func TestResolveProviders_ToDomainRoute(t *testing.T) {
	cfg := &EmailConfig{
		To:       []string{"User <user@gmail.com>"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid"}},
		},
	}
	got := resolveProviders(cfg)
	exp := []string{"sendgrid", "smtp"}
	if len(got) != len(exp) {
		t.Fatalf("expected %v got %v", exp, got)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Fatalf("expected %v got %v", exp, got)
		}
	}
}

func TestResolveProviders_LeastUsedSelection(t *testing.T) {
	// isolate send log per test
	tmpf, err := os.CreateTemp("", "sendlog-*.jsonl")
	if err != nil {
		t.Fatalf("cannot create temp log: %v", err)
	}
	_ = tmpf.Close()
	origSendLog := sendLogFile
	sendLogFile = tmpf.Name()
	defer func() { sendLogFile = origSendLog; os.Remove(tmpf.Name()) }()
	// create entries showing sendgrid used twice in last 24h, smtp none
	now := time.Now().UTC()
	entries := []string{
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Add(-2*time.Hour).Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
	}
	if err := os.WriteFile(sendLogFile, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}},
		},
	}
	got := resolveProviders(cfg)
	// smtp should be selected first because sendgrid has higher usage
	if got[0] != "smtp" {
		t.Fatalf("expected smtp first, got %v", got)
	}
}

func TestResolveProviders_SelectionWindow(t *testing.T) {
	defer withTempSendLog(t)()
	// sendgrid entry older than 2h; selection_window=1h so it shouldn't be counted
	now := time.Now().UTC()
	entries := []string{
		`{"timestamp":"` + now.Add(-2*time.Hour).Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"smtp","success":true,"recipients":["user@gmail.com"]}`,
	}
	if err := os.WriteFile(sendLogFile, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, SelectionWindow: time.Hour},
		},
	}
	got := resolveProviders(cfg)
	// sendgrid should be chosen because its usage in last hour is 0
	if got[0] != "sendgrid" {
		t.Fatalf("expected sendgrid first, got %v", got)
	}
}

func TestResolveProviders_RecencyHalfLife(t *testing.T) {
	defer withTempSendLog(t)()
	// sendgrid has a send 2h ago, smtp has a send now.
	now := time.Now().UTC()
	entries := []string{
		`{"timestamp":"` + now.Add(-2*time.Hour).Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"smtp","success":true,"recipients":["user@gmail.com"]}`,
	}
	if err := os.WriteFile(sendLogFile, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, SelectionWindow: 24 * time.Hour, RecencyHalfLife: time.Hour},
		},
	}
	got := resolveProviders(cfg)
	// sendgrid should be preferred because its recent send is older (lower weighted count)
	if got[0] != "sendgrid" {
		t.Fatalf("expected sendgrid first due to recency weighting, got %v", got)
	}
}

func TestResolveProviders_CostCapacityInfluence(t *testing.T) {
	// backup providerDefaults and restore later
	old := providerDefaults
	defer func() { providerDefaults = old }()
	RegisterProviderDefault("sendgrid", ProviderSetting{Host: "smtp.sendgrid.net", Port: 587, UseTLS: true, Capacity: 1, Cost: 2.0})
	RegisterProviderDefault("smtp", ProviderSetting{Host: "localhost", Port: 1025, UseTLS: false, Capacity: 10, Cost: 1.0})
	defer withTempSendLog(t)()
	// both providers have equal recent usage
	now := time.Now().UTC()
	entries := []string{
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"smtp","success":true,"recipients":["user@gmail.com"]}`,
	}
	if err := os.WriteFile(sendLogFile, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, SelectionWindow: 24 * time.Hour},
		},
	}
	got := resolveProviders(cfg)
	// smtp should be chosen because sendgrid has higher cost and low capacity
	if got[0] != "smtp" {
		t.Fatalf("expected smtp first due to cost/capacity, got %v", got)
	}
}

func TestResolveProviders_PerRouteCostCapacity(t *testing.T) {
	// per-route override should influence selection
	defer withTempSendLog(t)()
	now := time.Now().UTC()
	entries := []string{
		// equal recent usage
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"smtp","success":true,"recipients":["user@gmail.com"]}`,
	}
	if err := os.WriteFile(sendLogFile, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, SelectionWindow: 24 * time.Hour, ProviderCapacities: map[string]int{"sendgrid": 1}, ProviderCostOverrides: map[string]float64{"sendgrid": 2.0}},
		},
	}
	got := resolveProviders(cfg)
	// smtp should be chosen because sendgrid has higher cost and limited capacity per-route
	if got[0] != "smtp" {
		t.Fatalf("expected smtp first due to per-route cost/capacity, got %v", got)
	}
}

func TestResolveProviders_WeightedSelection(t *testing.T) {
	defer withTempSendLog(t)()
	now := time.Now().UTC()
	entries := []string{
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"sendgrid","success":true,"recipients":["user@gmail.com"]}`,
		`{"timestamp":"` + now.Format(time.RFC3339) + `","attempt":1,"provider":"smtp","success":true,"recipients":["user@gmail.com"]}`,
	}
	if err := os.WriteFile(sendLogFile, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write test log: %v", err)
	}
	cfg := &EmailConfig{
		To:       []string{"user@gmail.com"},
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, ProviderWeights: map[string]float64{"sendgrid": 2.0, "smtp": 1.0}},
		},
	}
	got := resolveProviders(cfg)
	// smtp should be chosen because its score (1*1.0=1) < sendgrid (1*2.0=2)
	if got[0] != "smtp" {
		t.Fatalf("expected smtp first due to weight, got %v", got)
	}
}

func TestResolveProviders_SubjectRegexRoute(t *testing.T) {
	cfg := &EmailConfig{
		Subject:  "Welcome!",
		Provider: "smtp",
		ProviderRoutes: []ProviderRoute{
			{SubjectRegex: "Welcome", ProviderPriority: []string{"sendinblue"}},
		},
	}
	got := resolveProviders(cfg)
	exp := []string{"sendinblue", "smtp"}
	if len(got) != len(exp) {
		t.Fatalf("expected %v got %v", exp, got)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Fatalf("expected %v got %v", exp, got)
		}
	}
}

func TestResolveProviders_NoMatchFallback(t *testing.T) {
	cfg := &EmailConfig{
		Provider: "smtp",
	}
	got := resolveProviders(cfg)
	exp := []string{"smtp"}
	if len(got) != len(exp) {
		t.Fatalf("expected %v got %v", exp, got)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Fatalf("expected %v got %v", exp, got)
		}
	}
}
