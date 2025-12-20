package main

import "strings"

type configEntry struct {
	original  string
	sanitized string
	value     any
	used      bool
}

type normalizedConfig struct {
	entries map[string][]*configEntry
}

func newNormalizedConfig(raw map[string]any) *normalizedConfig {
	entries := make(map[string][]*configEntry)
	for key, value := range raw {
		sanitized := sanitizeKey(key)
		e := &configEntry{original: key, sanitized: sanitized, value: value}
		entries[sanitized] = append(entries[sanitized], e)
	}
	return &normalizedConfig{entries: entries}
}

func (n *normalizedConfig) leftOverEntries() []*configEntry {
	var result []*configEntry
	for _, list := range n.entries {
		for _, entry := range list {
			if !entry.used {
				result = append(result, entry)
			}
		}
	}
	return result
}

func (n *normalizedConfig) leftovers() map[string]any {
	result := make(map[string]any)
	for _, entry := range n.leftOverEntries() {
		result[entry.original] = entry.value
	}
	return result
}

func (n *normalizedConfig) pullValue(canonical string) (any, bool) {
	if canonical == "" {
		return nil, false
	}
	if aliases, ok := fieldAliases[canonical]; ok {
		if val, ok := n.consumeAliases(aliases); ok {
			return val, true
		}
	}
	if val, ok := n.consumeExact(canonical); ok {
		return val, true
	}
	return n.consumeFuzzy(canonical)
}

func (n *normalizedConfig) consumeAliases(aliases []string) (any, bool) {
	for _, alias := range aliases {
		if val, ok := n.consumeExact(alias); ok {
			return val, true
		}
	}
	return nil, false
}

func (n *normalizedConfig) consumeExact(key string) (any, bool) {
	sanitized := sanitizeKey(key)
	if entries, ok := n.entries[sanitized]; ok {
		for _, entry := range entries {
			if entry.used {
				continue
			}
			entry.used = true
			return entry.value, true
		}
	}
	return nil, false
}

func (n *normalizedConfig) consumeFuzzy(target string) (any, bool) {
	token := sanitizeKey(target)
	if len(token) < 4 {
		return nil, false
	}
	for key, entries := range n.entries {
		if len(key) < 4 {
			continue
		}
		if !strings.Contains(key, token) && !strings.Contains(token, key) {
			continue
		}
		for _, entry := range entries {
			if entry.used {
				continue
			}
			entry.used = true
			return entry.value, true
		}
	}
	return nil, false
}
