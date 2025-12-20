package main

import (
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
