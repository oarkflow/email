package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
)

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

const placeholderMaxDepth = 5

var placeholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

func applyPlaceholders(cfg *EmailConfig, mode placeholderMode) error {
	resolver := newPlaceholderResolver(cfg)
	cfg.AdditionalData = resolver.expandObjectMap(cfg.AdditionalData)
	if err := resolver.Err(); err != nil {
		return err
	}

	for pass := 0; pass < 2; pass++ {
		resolver = newPlaceholderResolver(cfg)
		if mode == placeholderModeInitial && pass == 0 {
			cfg.From = strings.TrimSpace(resolver.expandString(cfg.From))
			cfg.FromName = strings.TrimSpace(resolver.expandString(cfg.FromName))
			cfg.EnvelopeFrom = strings.TrimSpace(resolver.expandString(cfg.EnvelopeFrom))
			cfg.ReturnPath = strings.TrimSpace(resolver.expandString(cfg.ReturnPath))
			cfg.Username = strings.TrimSpace(resolver.expandString(cfg.Username))
			cfg.Password = resolver.expandString(cfg.Password)
			cfg.APIKey = resolver.expandString(cfg.APIKey)
			cfg.APIToken = resolver.expandString(cfg.APIToken)
			cfg.Provider = strings.ToLower(strings.TrimSpace(resolver.expandString(cfg.Provider)))
			cfg.Transport = strings.ToLower(strings.TrimSpace(resolver.expandString(cfg.Transport)))
			cfg.Host = strings.TrimSpace(resolver.expandString(cfg.Host))
			cfg.Endpoint = strings.TrimSpace(resolver.expandString(cfg.Endpoint))
			cfg.HTTPAuth = strings.ToLower(strings.TrimSpace(resolver.expandString(cfg.HTTPAuth)))
			cfg.HTTPAuthHeader = strings.TrimSpace(resolver.expandString(cfg.HTTPAuthHeader))
			cfg.HTTPAuthQuery = strings.TrimSpace(resolver.expandString(cfg.HTTPAuthQuery))
			cfg.HTTPAuthPrefix = strings.TrimSpace(resolver.expandString(cfg.HTTPAuthPrefix))
			cfg.SMTPAuth = strings.ToLower(strings.TrimSpace(resolver.expandString(cfg.SMTPAuth)))
			cfg.AWSRegion = strings.TrimSpace(resolver.expandString(cfg.AWSRegion))
			cfg.AWSAccessKey = strings.TrimSpace(resolver.expandString(cfg.AWSAccessKey))
			cfg.AWSSecretKey = strings.TrimSpace(resolver.expandString(cfg.AWSSecretKey))
			cfg.AWSSessionToken = strings.TrimSpace(resolver.expandString(cfg.AWSSessionToken))
			cfg.ConfigurationSet = strings.TrimSpace(resolver.expandString(cfg.ConfigurationSet))
			cfg.HTMLTemplatePath = strings.TrimSpace(resolver.expandString(cfg.HTMLTemplatePath))
			cfg.TextTemplatePath = strings.TrimSpace(resolver.expandString(cfg.TextTemplatePath))
			cfg.BodyTemplatePath = strings.TrimSpace(resolver.expandString(cfg.BodyTemplatePath))
			cfg.ReplyTo = resolver.expandSlice(cfg.ReplyTo)
			cfg.To = resolver.expandSlice(cfg.To)
			cfg.CC = resolver.expandSlice(cfg.CC)
			cfg.BCC = resolver.expandSlice(cfg.BCC)
			cfg.ListUnsubscribe = resolver.expandSlice(cfg.ListUnsubscribe)
		}

		cfg.Subject = resolver.expandString(cfg.Subject)
		cfg.Body = resolver.expandString(cfg.Body)
		cfg.TextBody = resolver.expandString(cfg.TextBody)
		cfg.HTMLBody = resolver.expandString(cfg.HTMLBody)
		cfg.Endpoint = strings.TrimSpace(resolver.expandString(cfg.Endpoint))
		cfg.Headers = resolver.expandMap(cfg.Headers)
		cfg.QueryParams = resolver.expandMap(cfg.QueryParams)
		cfg.HTTPPayload = resolver.expandObjectMap(cfg.HTTPPayload)
		cfg.Tags = resolver.expandMap(cfg.Tags)
		cfg.Attachments = resolver.expandAttachments(cfg.Attachments)

		if err := resolver.Err(); err != nil {
			return err
		}
	}
	return nil
}

type placeholderResolver struct {
	values  map[string]string
	missing map[string]struct{}
}

func newPlaceholderResolver(cfg *EmailConfig) *placeholderResolver {
	return &placeholderResolver{
		values:  buildPlaceholderValues(cfg),
		missing: map[string]struct{}{},
	}
}

func (r *placeholderResolver) Err() error {
	if len(r.missing) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.missing))
	for k := range r.missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Errorf("unknown placeholders: %s", strings.Join(keys, ", "))
}

func (r *placeholderResolver) expandString(input string) string {
	if input == "" || !strings.Contains(input, "{{") {
		return input
	}
	result := input
	for depth := 0; depth < placeholderMaxDepth; depth++ {
		changed := false
		result = placeholderPattern.ReplaceAllStringFunc(result, func(match string) string {
			subs := placeholderPattern.FindStringSubmatch(match)
			if len(subs) < 2 {
				return ""
			}
			key := subs[1]
			if val, ok := r.lookup(key); ok {
				changed = true
				return val
			}
			r.logMissing(key)
			r.markMissing(key)
			return ""
		})
		if !changed || !strings.Contains(result, "{{") {
			break
		}
	}
	return result
}

func (r *placeholderResolver) expandSlice(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if expanded := strings.TrimSpace(r.expandString(value)); expanded != "" {
			result = append(result, expanded)
		}
	}
	return result
}

func (r *placeholderResolver) expandMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	for key, value := range values {
		values[key] = r.expandString(value)
	}
	return values
}

func (r *placeholderResolver) expandAttachments(list []Attachment) []Attachment {
	if len(list) == 0 {
		return list
	}
	result := make([]Attachment, 0, len(list))
	for _, att := range list {
		result = append(result, Attachment{
			Source:    strings.TrimSpace(r.expandString(att.Source)),
			Name:      r.expandString(att.Name),
			MIMEType:  r.expandString(att.MIMEType),
			Inline:    att.Inline,
			ContentID: r.expandString(att.ContentID),
		})
	}
	return result
}

func (r *placeholderResolver) expandObjectMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	expanded := r.expandInterface(input)
	if out, ok := expanded.(map[string]any); ok {
		return out
	}
	return input
}

func (r *placeholderResolver) expandInterface(value any) any {
	switch v := value.(type) {
	case string:
		return r.expandString(v)
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = r.expandInterface(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = r.expandInterface(item)
		}
		return result
	case []string:
		items := make([]any, len(v))
		for i, item := range v {
			items[i] = r.expandInterface(item)
		}
		return items
	default:
		return value
	}
}

func (r *placeholderResolver) lookup(raw string) (string, bool) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", false
	}
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, "env.") {
		name := strings.TrimSpace(raw[len("env."):])
		if name == "" {
			return "", false
		}
		if value, ok := os.LookupEnv(name); ok {
			logPlaceholderResolved("env."+name, value)
			return value, true
		}
		return "", false
	}
	if value, ok := r.values[lower]; ok {
		logPlaceholderResolved(lower, value)
		return value, true
	}
	return "", false
}

func (r *placeholderResolver) markMissing(key string) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return
	}
	r.missing[key] = struct{}{}
}

func (r *placeholderResolver) logMissing(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	log.Printf("[placeholders] missing value for %s", key)
}

func logPlaceholderResolved(key, value string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	log.Printf("[placeholders] %s => %s", key, maskPlaceholderValue(key, value))
}

func maskPlaceholderValue(key, value string) string {
	lower := strings.ToLower(key)
	if lower == "" {
		return value
	}
	sensitive := strings.Contains(lower, "pass") || strings.Contains(lower, "pwd") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "auth")
	if sensitive {
		if value == "" {
			return "(empty)"
		}
		return "[redacted]"
	}
	if len(value) > 200 {
		return value[:200] + "..."
	}
	return value
}
