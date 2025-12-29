package main

import (
	"os"
	"testing"
)

func TestGreedyBatchOptimizer_RespectsPerRouteCapacities(t *testing.T) {
	// isolate send log per test
	tmpf, err := os.CreateTemp("", "sendlog-*.jsonl")
	if err != nil {
		t.Fatalf("cannot create temp log: %v", err)
	}
	_ = tmpf.Close()
	origSendLog := sendLogFile
	sendLogFile = tmpf.Name()
	defer func() { sendLogFile = origSendLog; os.Remove(tmpf.Name()) }()

	opt := &GreedyBatchOptimizer{}
	cfg := &EmailConfig{
		To:               []string{"user@gmail.com"},
		ProviderPriority: []string{"sendgrid", "smtp"},
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, ProviderCapacities: map[string]int{"sendgrid": 2, "smtp": 100}, HourlyLimit: 0},
		},
	}
	jobs := []*ScheduledEmail{
		{ID: "job1", Config: cfg},
		{ID: "job2", Config: cfg},
		{ID: "job3", Config: cfg},
	}
	alloc := opt.AllocateJobs(jobs)
	if len(alloc) != 3 {
		t.Fatalf("expected 3 allocations got %d", len(alloc))
	}
	countSendgrid := 0
	countSMTP := 0
	for _, p := range alloc {
		if p == "sendgrid" {
			countSendgrid++
		} else if p == "smtp" {
			countSMTP++
		}
	}
	if countSendgrid != 2 {
		t.Fatalf("expected 2 sendgrid allocations got %d", countSendgrid)
	}
	if countSMTP != 1 {
		t.Fatalf("expected 1 smtp allocation got %d", countSMTP)
	}
}

func TestGreedyBatchOptimizer_PrefersLowerCostWhenTied(t *testing.T) {
	// isolate send log per test
	tmpf, err := os.CreateTemp("", "sendlog-*.jsonl")
	if err != nil {
		t.Fatalf("cannot create temp log: %v", err)
	}
	_ = tmpf.Close()
	origSendLog := sendLogFile
	sendLogFile = tmpf.Name()
	defer func() { sendLogFile = origSendLog; os.Remove(tmpf.Name()) }()

	opt := &GreedyBatchOptimizer{}
	cfg := &EmailConfig{
		To:               []string{"user@gmail.com"},
		ProviderPriority: []string{"sendgrid", "smtp"},
		ProviderRoutes: []ProviderRoute{
			{ToDomains: []string{"gmail.com"}, ProviderPriority: []string{"sendgrid", "smtp"}, ProviderCostOverrides: map[string]float64{"sendgrid": 2.0, "smtp": 1.0}, HourlyLimit: 0},
		},
	}
	jobs := []*ScheduledEmail{{ID: "job1", Config: cfg}}
	alloc := opt.AllocateJobs(jobs)
	if alloc["job1"] != "smtp" {
		t.Fatalf("expected smtp due to lower cost, got %s", alloc["job1"])
	}
}
