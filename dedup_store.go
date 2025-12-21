package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

const dedupStoreFile = "send_dedup.json"

var (
	dedupMu     sync.Mutex
	dedupCache  map[string]time.Time
	dedupLoaded bool
)

func dedupKeyExists(key string) bool {
	if key == "" {
		return false
	}
	dedupMu.Lock()
	defer dedupMu.Unlock()
	if !dedupLoaded {
		loadDedupLocked()
	}
	if dedupCache == nil {
		return false
	}
	_, ok := dedupCache[key]
	return ok
}

func markDedupKey(key string) {
	if key == "" {
		return
	}
	dedupMu.Lock()
	defer dedupMu.Unlock()
	if !dedupLoaded {
		loadDedupLocked()
	}
	if dedupCache == nil {
		dedupCache = map[string]time.Time{}
	}
	dedupCache[key] = time.Now().UTC()
	writeDedupLocked()
}

func loadDedupLocked() {
	data, err := os.ReadFile(dedupStoreFile)
	if err != nil {
		if os.IsNotExist(err) {
			dedupCache = map[string]time.Time{}
			dedupLoaded = true
			return
		}
		log.Printf("dedup: cannot read store: %v", err)
		return
	}
	var raw map[string]time.Time
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Printf("dedup: cannot decode store: %v", err)
		return
	}
	dedupCache = raw
	dedupLoaded = true
}

func writeDedupLocked() {
	data, err := json.MarshalIndent(dedupCache, "", "  ")
	if err != nil {
		log.Printf("dedup: cannot encode store: %v", err)
		return
	}
	if err := os.WriteFile(dedupStoreFile, data, 0o644); err != nil {
		log.Printf("dedup: cannot write store: %v", err)
	}
}
