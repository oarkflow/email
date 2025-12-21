package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
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
			// Allow missing {{step}} if a workflow is present; individual steps will provide it
			missing := resolver.MissingKeys()
			if len(missing) == 1 && missing[0] == "step" {
				if cfg.AdditionalData != nil {
					if _, ok := cfg.AdditionalData["workflow_steps"]; ok {
						return nil
					}
					if _, ok := cfg.AdditionalData["workflow_definition"]; ok {
						return nil
					}
					if wf, ok := cfg.AdditionalData["workflow"]; ok {
						switch wf.(type) {
						case []any:
							return nil
						case string:
							// legacy string-based workflows (e.g., "welcome")
							return nil
						}
					}
				}
			}
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

func buildPlaceholderValues(cfg *EmailConfig) map[string]string {
	values := map[string]string{}
	now := time.Now()
	registerValue(values, now.Format(time.RFC3339), true, "now", "datetime")
	registerValue(values, now.Format("2006-01-02"), true, "today", "date")
	registerValue(values, fmt.Sprintf("%d", now.Unix()), true, "timestamp")
	registerValue(values, cfg.Provider, true, "provider", "service")
	registerValue(values, cfg.Transport, true, "transport", "type")
	registerValue(values, cfg.HTTPMethod, true, "http_method", "verb")
	registerValue(values, cfg.Endpoint, true, "endpoint", "url", "api_url")
	registerValue(values, cfg.Host, true, "host", "server", "smtp_host")
	registerValue(values, cfg.From, true, "from", "sender", "from_email")
	registerValue(values, cfg.FromName, true, "from_name", "sender_name")
	registerValue(values, cfg.EnvelopeFrom, true, "envelope_from", "return_path")
	registerValue(values, cfg.Username, true, "username", "user", "login")
	registerValue(values, cfg.Password, true, "password", "pass")
	registerValue(values, cfg.APIKey, true, "api_key", "key")
	registerValue(values, cfg.APIToken, true, "api_token", "token", "bearer")
	registerValue(values, cfg.HTTPAuth, true, "http_auth", "auth")
	registerValue(values, cfg.Subject, true, "subject", "title")
	registerValue(values, cfg.Body, true, "body", "message", "content", "raw_body")
	registerValue(values, cfg.TextBody, true, "text_body", "text", "plain_text")
	registerValue(values, cfg.HTMLBody, true, "html_body", "html")
	registerValue(values, cfg.ConfigurationSet, true, "configuration_set", "config_set")
	registerValue(values, cfg.AWSRegion, true, "aws_region", "region")
	registerValue(values, cfg.AWSAccessKey, false, "aws_access_key", "access_key")
	registerValue(values, cfg.AWSSecretKey, false, "aws_secret_key", "secret_key")
	registerValue(values, cfg.AWSSessionToken, false, "aws_session_token", "session_token")
	registerSliceValue(values, cfg.To, true, "to", "recipients", "send_to")
	registerSliceValue(values, cfg.CC, true, "cc")
	registerSliceValue(values, cfg.BCC, true, "bcc")
	registerSliceValue(values, cfg.ReplyTo, true, "reply_to")
	registerSliceValue(values, cfg.ListUnsubscribe, true, "list_unsubscribe")
	if len(cfg.Tags) > 0 {
		var tagParts []string
		for k, v := range cfg.Tags {
			tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(tagParts)
		registerValue(values, strings.Join(tagParts, ";"), true, "tags", "ses_tags")
	}
	if cfg.AdditionalData != nil {
		flattenAdditionalData(values, cfg.AdditionalData)
	}
	return values
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

// MissingKeys returns the current unknown placeholder keys in a deterministic order.
func (r *placeholderResolver) MissingKeys() []string {
	if len(r.missing) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.missing))
	for k := range r.missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
}

func logPlaceholderResolved(key, value string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
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
