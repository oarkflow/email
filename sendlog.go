package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

const (
	sendLogFile     = "send_log.jsonl"
	jobResultDBFile = "send_results.json"
)

type JobResult string

const (
	JobResultSuccess JobResult = "success"
	JobResultFailed  JobResult = "failed"
	JobResultSkipped JobResult = "skipped"
	JobResultBlocked JobResult = "blocked"
)

type SendLogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	JobID      string    `json:"job_id,omitempty"`
	Step       string    `json:"step,omitempty"`
	Attempt    int       `json:"attempt"`
	Provider   string    `json:"provider"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	Recipients []string  `json:"recipients,omitempty"`
}

var (
	sendLogMu      sync.Mutex
	jobResultMu    sync.Mutex
	jobResultCache map[string]JobResult
	jobResultsInit bool
)

func recordSendAttempt(ctx *SendContext, cfg *EmailConfig, attempt int, err error) {
	entry := SendLogEntry{
		Timestamp:  time.Now().UTC(),
		Attempt:    attempt,
		Provider:   cfg.ProviderOrHost(),
		Success:    err == nil,
		Recipients: append([]string(nil), cfg.To...),
	}
	if ctx != nil {
		entry.JobID = ctx.JobID
		entry.Step = ctx.Step
	}
	if err != nil {
		entry.Error = err.Error()
	}
	appendSendLog(entry)
}

func appendSendLog(entry SendLogEntry) {
	sendLogMu.Lock()
	defer sendLogMu.Unlock()
	f, err := os.OpenFile(sendLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("sendlog: cannot open log file: %v", err)
		return
	}
	defer f.Close()
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("sendlog: cannot marshal entry: %v", err)
		return
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		log.Printf("sendlog: cannot write entry: %v", err)
	}
}

func recordJobResult(jobID string, result JobResult) {
	if jobID == "" {
		return
	}
	jobResultMu.Lock()
	defer jobResultMu.Unlock()
	if !jobResultsInit {
		loadJobResultsLocked()
	}
	if jobResultCache == nil {
		jobResultCache = map[string]JobResult{}
	}
	jobResultCache[jobID] = result
	writeJobResultsLocked()
}

func getJobResult(jobID string) (JobResult, bool) {
	if jobID == "" {
		return "", false
	}
	jobResultMu.Lock()
	defer jobResultMu.Unlock()
	if !jobResultsInit {
		loadJobResultsLocked()
	}
	res, ok := jobResultCache[jobID]
	return res, ok
}

func loadJobResultsLocked() {
	data, err := os.ReadFile(jobResultDBFile)
	if err != nil {
		if os.IsNotExist(err) {
			jobResultCache = map[string]JobResult{}
			jobResultsInit = true
			return
		}
		log.Printf("sendlog: cannot read results: %v", err)
		jobResultCache = map[string]JobResult{}
		jobResultsInit = true
		return
	}
	if err := json.Unmarshal(data, &jobResultCache); err != nil {
		log.Printf("sendlog: cannot decode results: %v", err)
		jobResultCache = map[string]JobResult{}
	}
	jobResultsInit = true
}

func writeJobResultsLocked() {
	data, err := json.MarshalIndent(jobResultCache, "", "  ")
	if err != nil {
		log.Printf("sendlog: cannot encode results: %v", err)
		return
	}
	if err := os.WriteFile(jobResultDBFile, data, 0o644); err != nil {
		log.Printf("sendlog: cannot write results: %v", err)
	}
}
