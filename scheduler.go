package main

import (
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
					if cfgCopy.AdditionalData == nil {
						cfgCopy.AdditionalData = map[string]any{}
					}
					for k, v := range j.Meta {
						if strings.TrimSpace(k) == "" {
							continue
						}
						cfgCopy.AdditionalData[k] = v
					}

					// Re-apply placeholders (post-finalize) so templates can consume job meta like {{step}}
					if err := applyPlaceholders(&cfgCopy, placeholderModePostFinalize); err != nil {
						log.Printf("scheduler: job %s placeholder error: %v", j.ID, err)
						j.Attempts++
						if err := s.store.Update(j); err != nil {
							log.Printf("scheduler: cannot update job %s: %v", j.ID, err)
						}
						return
					}

					if err := sendEmail(&cfgCopy); err != nil {
						log.Printf("scheduler: job %s failed: %v", j.ID, err)
						// increase attempts and persist
						j.Attempts++
						if err := s.store.Update(j); err != nil {
							log.Printf("scheduler: cannot update job %s: %v", j.ID, err)
						}
						return
					}
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
