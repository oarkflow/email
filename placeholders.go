package main

import "strings"

type placeholderMode int

const (
	placeholderModeInitial placeholderMode = iota
	placeholderModePostFinalize
)

func normalizePlaceholderKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return ""
	}
	var b strings.Builder
	last := rune(0)
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			last = r
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			last = r
		case r == '.' || r == '_':
			if last == '.' || last == '_' || b.Len() == 0 {
				continue
			}
			b.WriteRune(r)
			last = r
		default:
			if last == '_' {
				continue
			}
			if b.Len() == 0 {
				continue
			}
			b.WriteRune('_')
			last = '_'
		}
	}
	return strings.Trim(b.String(), "._")
}
