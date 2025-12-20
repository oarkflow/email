package main

import (
	"fmt"
	"log"
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

	for _, sdef := range steps {
		cfgCopy := *base
		// modify subject to identify the step
		cfgCopy.Subject = sdef.subj
		runAt := now.Add(sdef.offset)
		job, err := s.Schedule(&cfgCopy, runAt, sdef.meta)
		if err != nil {
			return err
		}
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

		cfgCopy := *base
		// apply overrides
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

		meta := map[string]any{"step_index": i}
		if name, ok := stepMap["name"].(string); ok && name != "" {
			meta["name"] = name
			// provide a common `step` key for templates that refer to {{step}}
			meta["step"] = name
		}
		job, err := s.Schedule(&cfgCopy, runAt, meta)
		if err != nil {
			return err
		}
		log.Printf("workflow: scheduled step %v at %s (job=%s)", meta, job.RunAt, job.ID)
	}
	return nil
}
