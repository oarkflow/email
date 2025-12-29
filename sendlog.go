package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

var (
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

// countSuccessesFromReader scans a JSONL stream and counts successful sends matching providers
// and recipient domains. If providers is nil or empty, matches any provider. If toDomains is nil/empty,
// counts all recipients.
func countSuccessesFromReader(r io.Reader, providers []string, since time.Time, toDomains []string) (int, error) {
	scanner := bufio.NewScanner(r)
	count := 0
	provSet := map[string]bool{}
	for _, p := range providers {
		provSet[strings.ToLower(strings.TrimSpace(p))] = true
	}
	for scanner.Scan() {
		var e SendLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if !e.Success {
			continue
		}
		if e.Timestamp.Before(since) {
			continue
		}
		if len(providers) > 0 {
			if _, ok := provSet[strings.ToLower(strings.TrimSpace(e.Provider))]; !ok {
				continue
			}
		}
		// If no domain filter, count it
		if len(toDomains) == 0 {
			count++
			continue
		}
		// check recipients domains
		matched := false
		for _, rcpt := range e.Recipients {
			if d := extractDomain(rcpt); d != "" {
				for _, td := range toDomains {
					if strings.EqualFold(strings.TrimSpace(td), d) {
						matched = true
						break
					}
				}
			}
			if matched {
				count++
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

// countSuccessesSince reads the persistent send log and counts successes matching criteria.
func countSuccessesSince(providers []string, since time.Time, toDomains []string) (int, error) {
	f, err := os.Open(sendLogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	return countSuccessesFromReader(f, providers, since, toDomains)
}

// weightedUsageFromReader computes a recency-weighted usage score from a JSONL stream.
// It applies exponential decay with the provided halfLife (duration). Entries older than 'since' are ignored.
// Returns mapping of provider -> score.
func weightedUsageFromReader(r io.Reader, providers []string, since time.Time, toDomains []string, halfLife time.Duration) (map[string]float64, error) {
	scanner := bufio.NewScanner(r)
	scores := map[string]float64{}
	provSet := map[string]bool{}
	for _, p := range providers {
		provSet[strings.ToLower(strings.TrimSpace(p))] = true
	}
	if halfLife <= 0 {
		// default half-life is 6 hours
		halfLife = 6 * time.Hour
	}
	ln2 := math.Ln2
	for scanner.Scan() {
		var e SendLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if !e.Success {
			continue
		}
		if e.Timestamp.Before(since) {
			continue
		}
		prov := strings.ToLower(strings.TrimSpace(e.Provider))
		if len(providers) > 0 {
			if _, ok := provSet[prov]; !ok {
				continue
			}
		}
		// If no domain filter, count it
		if len(toDomains) == 0 {
			age := time.Since(e.Timestamp)
			weight := math.Exp(-ln2 * age.Hours() / halfLife.Hours())
			scores[prov] += weight
			continue
		}
		// check recipients domains
		matched := false
		for _, rcpt := range e.Recipients {
			if d := extractDomain(rcpt); d != "" {
				for _, td := range toDomains {
					if strings.EqualFold(strings.TrimSpace(td), d) {
						matched = true
						break
					}
				}
			}
			if matched {
				age := time.Since(e.Timestamp)
				weight := math.Exp(-ln2 * age.Hours() / halfLife.Hours())
				scores[prov] += weight
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return scores, nil
}

// weightedUsageSince reads the persistent send log and computes recency-weighted usage scores.
func weightedUsageSince(providers []string, since time.Time, toDomains []string, halfLife time.Duration) (map[string]float64, error) {
	f, err := os.Open(sendLogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]float64{}, nil
		}
		return nil, err
	}
	defer f.Close()
	return weightedUsageFromReader(f, providers, since, toDomains, halfLife)
}
