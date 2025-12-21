package main

import "testing"

func TestStepPlaceholderResolution(t *testing.T) {
	cfg := &EmailConfig{
		HTMLBody:       "this email is part of the <strong>{{step}}</strong> step",
		TextBody:       "step={{step}}",
		AdditionalData: map[string]any{"step": "welcome"},
	}
	if err := applyPlaceholders(cfg, placeholderModePostFinalize); err != nil {
		t.Fatalf("applyPlaceholders returned error: %v", err)
	}
	if cfg.HTMLBody != "this email is part of the <strong>welcome</strong> step" {
		t.Fatalf("step placeholder not resolved, got %q", cfg.HTMLBody)
	}
}
