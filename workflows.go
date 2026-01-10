package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// ScheduleWelcomeWorkflow creates a sample 4-step onboarding workflow for a user.
// Steps:
//   - welcome: now
//   - credentials: now + 1 minute
//   - walkthrough: now + 1 hour
//   - idle reminder: now + 1 week
func ScheduleWelcomeWorkflow(s *Scheduler, base *EmailConfig) error {
	now := time.Now()
	steps := []struct {
		offset time.Duration
		meta   map[string]any
		subj   string
	}{
		{0 * time.Second, map[string]any{"step": "welcome"}, "Welcome!"},
		{1 * time.Minute, map[string]any{"step": "credentials"}, "Your login credentials"},
		{1 * time.Hour, map[string]any{"step": "walkthrough"}, "Product walkthrough"},
		{7 * 24 * time.Hour, map[string]any{"step": "idle_reminder"}, "We miss you"},
	}

	var lastJobID string
	for idx, sdef := range steps {
		cfgCopy := *base
		cfgCopy.AdditionalData = cloneAdditionalData(base.AdditionalData)
		// modify subject to identify the step
		cfgCopy.Subject = sdef.subj
		runAt := now.Add(sdef.offset)

		// Add step metadata to AdditionalData so it's available at execution time
		if cfgCopy.AdditionalData == nil {
			cfgCopy.AdditionalData = map[string]any{}
		}
		meta := map[string]any{}
		for k, v := range sdef.meta {
			cfgCopy.AdditionalData[k] = v
			meta[k] = v
		}
		meta["step_index"] = idx
		if lastJobID != "" {
			meta["prev_job_id"] = lastJobID
			meta["require_last_success"] = true
		}
		if _, ok := meta["skip_ahead"]; !ok {
			meta["skip_ahead"] = false
		}
		for k, v := range meta {
			cfgCopy.AdditionalData[k] = v
		}

		job, err := s.Schedule(&cfgCopy, runAt, meta)
		if err != nil {
			return err
		}
		lastJobID = job.ID
		log.Printf("workflow: scheduled %s at %s (job=%s)", sdef.meta["step"], job.RunAt, job.ID)
	}
	return nil
}

// ScheduleGenericWorkflow schedules a workflow defined by an array of steps in `def`.
// Expected step format (each step is a map):
//
//	{
//	  "name": "welcome",
//	  "delay_seconds": 0,             // or "run_at": "2025-12-21T00:00:00Z"
//	  "subject": "Welcome!",
//	  "body": "Hello",
//	  "to": ["user@example.org"],   // optional override
//	  "provider_priority": ["sendgrid", "smtp"], // optional per-step
//	  "retry_count": 3,
//	  "retry_delay_seconds": 2,
//	  "max_retry_delay_seconds": 10
//	}
func ScheduleGenericWorkflow(s *Scheduler, base *EmailConfig, def any) error {
	arr, ok := def.([]any)
	if !ok {
		return fmt.Errorf("workflow definition must be an array of steps")
	}
	now := time.Now()
	var lastJobID string
	for i, raw := range arr {
		stepMap, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("workflow step %d must be an object", i)
		}
		// compute runAt
		runAt := now
		if v, ok := stepMap["run_at"].(string); ok && v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				runAt = t
			}
		} else if v, ok := stepMap["delay_seconds"].(float64); ok {
			runAt = now.Add(time.Duration(v) * time.Second)
		}

		// Create a copy of the base config for this step
		cfgCopy := *base
		cfgCopy.AdditionalData = cloneAdditionalData(base.AdditionalData)

		// Apply step-specific overrides BEFORE template parsing
		// This ensures that step-specific subjects, bodies, etc. are available during placeholder resolution
		if subj, ok := stepMap["subject"].(string); ok && subj != "" {
			cfgCopy.Subject = subj
		}
		if body, ok := stepMap["body"].(string); ok {
			cfgCopy.Body = body
			cfgCopy.TextBody = body
		}
		if html, ok := stepMap["html_body"].(string); ok {
			cfgCopy.HTMLBody = html
		}
		if toArr, ok := stepMap["to"].([]any); ok && len(toArr) > 0 {
			var tos []string
			for _, e := range toArr {
				if s, ok := e.(string); ok && s != "" {
					tos = append(tos, s)
				}
			}
			if len(tos) > 0 {
				cfgCopy.To = tos
			}
		}
		if pp, ok := stepMap["provider_priority"].([]any); ok && len(pp) > 0 {
			var pri []string
			for _, e := range pp {
				if s, ok := e.(string); ok && s != "" {
					pri = append(pri, s)
				}
			}
			if len(pri) > 0 {
				cfgCopy.ProviderPriority = pri
			}
		}
		if rc, ok := stepMap["retry_count"].(float64); ok {
			cfgCopy.RetryCount = int(rc)
		}
		if rd, ok := stepMap["retry_delay_seconds"].(float64); ok {
			cfgCopy.RetryDelay = time.Duration(rd) * time.Second
		}
		if mrd, ok := stepMap["max_retry_delay_seconds"].(float64); ok {
			cfgCopy.MaxRetryDelay = time.Duration(mrd) * time.Second
		}

		// Create metadata for this step
		meta := map[string]any{"step_index": i}
		if lastJobID != "" {
			meta["prev_job_id"] = lastJobID
		}
		if name, ok := stepMap["name"].(string); ok && name != "" {
			meta["name"] = name
			meta["step"] = name
		}
		if stepLabel, ok := stepMap["step"].(string); ok && stepLabel != "" {
			meta["step"] = stepLabel
		}
		requireLast := false
		if rawReq, ok := stepMap["require_last_success"]; ok {
			requireLast = normalizeBool(rawReq)
		} else if lastJobID != "" {
			// Default to requiring last success for subsequent steps
			requireLast = true
		}
		if requireLast {
			meta["require_last_success"] = true
			skipAhead := false
			if rawSkip, ok := stepMap["skip_ahead"]; ok {
				skipAhead = normalizeBool(rawSkip)
			}
			meta["skip_ahead"] = skipAhead
		}
		if _, ok := meta["step"].(string); !ok || strings.TrimSpace(fmt.Sprint(meta["step"])) == "" {
			meta["step"] = fmt.Sprintf("step-%d", i)
		}
		// Add step metadata to AdditionalData so it's available at execution time
		if cfgCopy.AdditionalData == nil {
			cfgCopy.AdditionalData = map[string]any{}
		}
		for k, v := range meta {
			cfgCopy.AdditionalData[k] = v
		}
		job, err := s.Schedule(&cfgCopy, runAt, meta)
		if err != nil {
			return err
		}
		lastJobID = job.ID
		log.Printf("workflow: scheduled step %v at %s (job=%s)", meta, job.RunAt, job.ID)
	}
	return nil
}
