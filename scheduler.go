package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ScheduledEmail represents an email scheduled to be sent at a certain time.
type ScheduledEmail struct {
	ID       string         `json:"id"`
	Config   *EmailConfig   `json:"config"`
	RunAt    time.Time      `json:"run_at"`
	Attempts int            `json:"attempts"`
	Meta     map[string]any `json:"meta,omitempty"`
}

// Scheduler is a simple in-process scheduler with pluggable persistence.
type Scheduler struct {
	store    JobStore
	mu       sync.Mutex
	running  bool
	stop     chan struct{}
	wg       sync.WaitGroup
	interval time.Duration
}

// NewScheduler creates a scheduler with the provided store and polling interval.
func NewScheduler(store JobStore, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Scheduler{store: store, stop: make(chan struct{}), interval: interval}
}

// Start begins the scheduler loop. It runs until Stop() is called.
func (s *Scheduler) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler already running")
	}
	s.running = true
	s.mu.Unlock()

	log.Println("scheduler starting")
	s.wg.Add(1)
	go s.runLoop()
	return nil
}

// Stop signals the scheduler to stop and waits for it to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stop)
	s.wg.Wait()
	log.Println("scheduler stopped")
}

func (s *Scheduler) runLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			jobs, err := s.store.ListDue(now)
			if err != nil {
				log.Printf("scheduler: error listing due jobs: %v", err)
				continue
			}
			for _, job := range jobs {
				// execute each job in its own goroutine
				j := job
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					log.Printf("scheduler: executing job %s (run_at=%s)", j.ID, j.RunAt)

					// Make a local copy of the config and merge job meta into AdditionalData
					cfgCopy := *j.Config
					cfgCopy.AdditionalData = cloneAdditionalData(j.Config.AdditionalData)
					if cfgCopy.AdditionalData == nil {
						cfgCopy.AdditionalData = map[string]any{}
					}
					ctx := buildSendContext(j)
					if ctx.RequireLastSuccess && ctx.PrevJobID != "" {
						if res, ok := getJobResult(ctx.PrevJobID); ok {
							if res != JobResultSuccess {
								handleDependencyFailure(ctx, s, j, res)
								return
							}
						} else {
							log.Printf("scheduler: job %s waiting for dependency %s", j.ID, ctx.PrevJobID)
							return
						}
					}

					for k, v := range j.Meta {
						if strings.TrimSpace(k) == "" {
							continue
						}
						cfgCopy.AdditionalData[k] = v
					}

					if err := sendEmail(&cfgCopy, ctx); err != nil {
						if errors.Is(err, errDeduplicated) {
							log.Printf("scheduler: job %s skipped due to deduplication", j.ID)
							recordJobResult(j.ID, JobResultSkipped)
							if err := s.store.Delete(j.ID); err != nil {
								log.Printf("scheduler: cannot delete job %s: %v", j.ID, err)
							}
							return
						}
						log.Printf("scheduler: job %s failed: %v", j.ID, err)
						// increase attempts and persist
						j.Attempts++
						if err := s.store.Update(j); err != nil {
							log.Printf("scheduler: cannot update job %s: %v", j.ID, err)
						}
						recordJobResult(j.ID, JobResultFailed)
						return
					}
					recordJobResult(j.ID, JobResultSuccess)
					// success -> remove job
					if err := s.store.Delete(j.ID); err != nil {
						log.Printf("scheduler: cannot delete job %s: %v", j.ID, err)
					}
				}()
			}
		}
	}
}

// Schedule schedules a job to run at the given time and persists it.
func (s *Scheduler) Schedule(cfg *EmailConfig, runAt time.Time, meta map[string]any) (*ScheduledEmail, error) {
	job := &ScheduledEmail{ID: randomBoundary("job"), Config: cfg, RunAt: runAt.UTC(), Attempts: 0, Meta: meta}
	if err := s.store.Add(job); err != nil {
		return nil, err
	}
	return job, nil
}

// ScheduleNow schedules a job to run as soon as possible.
func (s *Scheduler) ScheduleNow(cfg *EmailConfig, meta map[string]any) (*ScheduledEmail, error) {
	return s.Schedule(cfg, time.Now().UTC(), meta)
}

func buildSendContext(job *ScheduledEmail) *SendContext {
	ctx := &SendContext{JobID: job.ID}
	if job.Meta == nil {
		return ctx
	}
	if idx, ok := job.Meta["step_index"]; ok {
		ctx.StepIndex = asInt(idx)
	}
	if step, ok := job.Meta["step"].(string); ok && step != "" {
		ctx.Step = step
	} else if name, ok := job.Meta["name"].(string); ok && name != "" {
		ctx.Step = name
	}
	if prev, ok := job.Meta["prev_job_id"].(string); ok {
		ctx.PrevJobID = prev
	}
	if require, ok := job.Meta["require_last_success"]; ok {
		ctx.RequireLastSuccess = normalizeBool(require)
	}
	if skip, ok := job.Meta["skip_ahead"]; ok {
		ctx.SkipAhead = normalizeBool(skip)
	}
	return ctx
}

func handleDependencyFailure(ctx *SendContext, s *Scheduler, job *ScheduledEmail, prev JobResult) {
	status := JobResultBlocked
	action := "blocking"
	if ctx.SkipAhead {
		status = JobResultSkipped
		action = "skipping"
	}
	log.Printf("scheduler: %s job %s (step=%s) because dependency %s finished with %s", action, job.ID, ctx.Step, ctx.PrevJobID, prev)
	recordJobResult(job.ID, status)
	if err := s.store.Delete(job.ID); err != nil {
		log.Printf("scheduler: cannot delete job %s: %v", job.ID, err)
	}
}

func asInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
