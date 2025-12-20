package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"sort"
	"sync"
	"time"
)

// JobStore defines persistence operations required by the scheduler.
type JobStore interface {
	Add(job *ScheduledEmail) error
	Update(job *ScheduledEmail) error
	Delete(id string) error
	ListDue(before time.Time) ([]*ScheduledEmail, error)
	ListAll() ([]*ScheduledEmail, error)
}

// FileJobStore is a simple JSON-file-backed store for scheduled jobs.
// It uses coarse-grained locking so it's safe for single-process use.
type FileJobStore struct {
	path string
	mu   sync.Mutex
}

func NewFileJobStore(path string) *FileJobStore {
	return &FileJobStore{path: path}
}

func (s *FileJobStore) loadAll() ([]*ScheduledEmail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return []*ScheduledEmail{}, nil
	}
	b, err := ioutil.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var jobs []*ScheduledEmail
	if err := json.Unmarshal(b, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *FileJobStore) persistAll(jobs []*ScheduledEmail) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.path, b, 0644)
}

func (s *FileJobStore) Add(job *ScheduledEmail) error {
	jobs, err := s.loadAll()
	if err != nil {
		return err
	}
	jobs = append(jobs, job)
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].RunAt.Before(jobs[j].RunAt) })
	return s.persistAll(jobs)
}

func (s *FileJobStore) Update(job *ScheduledEmail) error {
	jobs, err := s.loadAll()
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].ID == job.ID {
			jobs[i] = job
			return s.persistAll(jobs)
		}
	}
	return os.ErrNotExist
}

func (s *FileJobStore) Delete(id string) error {
	jobs, err := s.loadAll()
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].ID == id {
			jobs = append(jobs[:i], jobs[i+1:]...)
			return s.persistAll(jobs)
		}
	}
	return os.ErrNotExist
}

func (s *FileJobStore) ListDue(before time.Time) ([]*ScheduledEmail, error) {
	jobs, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	now := before.UTC()
	var due []*ScheduledEmail
	for _, j := range jobs {
		if !j.RunAt.After(now) {
			due = append(due, j)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].RunAt.Before(due[j].RunAt) })
	return due, nil
}

func (s *FileJobStore) ListAll() ([]*ScheduledEmail, error) {
	return s.loadAll()
}
