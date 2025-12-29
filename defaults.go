package main

import (
	"net/http"
	"sync"
)

// ProviderSetting holds transport and selection metadata used by defaults and tests.
type ProviderSetting struct {
	Host      string
	Port      int
	UseTLS    bool
	UseSSL    bool
	Transport string
	Endpoint  string
	Capacity  int
	Cost      float64
}

// providerDefaults contains a small set of sensible defaults for known providers.
var providerDefaults = map[string]ProviderSetting{
	"sendgrid": {Host: "smtp.sendgrid.net", Port: 587, UseTLS: true, Transport: "smtp", Endpoint: "https://api.sendgrid.com/v3/mail/send", Capacity: 1000, Cost: 0.5},
	"resend":   {Host: "smtp.resend.com", Port: 587, UseTLS: true, Transport: "smtp", Endpoint: "https://api.resend.com/emails", Capacity: 1000, Cost: 0.3},
	"postmark": {Host: "smtp.postmarkapp.com", Port: 587, UseTLS: true, Transport: "smtp", Endpoint: "https://api.postmarkapp.com/email", Capacity: 1000, Cost: 0.4},
	"mailgun":  {Host: "smtp.mailgun.org", Port: 587, UseTLS: true, Transport: "smtp", Endpoint: "https://api.mailgun.net/v3", Capacity: 1000, Cost: 0.4},
	"aws_ses":  {Host: "email-smtp.us-east-1.amazonaws.com", Port: 465, UseTLS: true, Transport: "smtp", Endpoint: "https://email.us-east-1.amazonaws.com", Capacity: 5000, Cost: 0.1},
	"smtp":     {Host: "localhost", Port: 1025, UseTLS: false, Transport: "smtp", Capacity: 0, Cost: 0.0},
	"gmail":    {Host: "smtp.gmail.com", Port: 587, UseTLS: true, Transport: "smtp", Capacity: 500, Cost: 0.0},
	"outlook":  {Host: "smtp-mail.outlook.com", Port: 587, UseTLS: true, Transport: "smtp", Capacity: 500, Cost: 0.0},
}

// RegisterProviderDefault allows tests or runtime code to override/add provider defaults.
func RegisterProviderDefault(name string, s ProviderSetting) {
	providerDefaults[name] = s
}

// HTTPProviderProfile contains lightweight HTTP hints used to populate configs.
type HTTPProviderProfile struct {
	Endpoint      string
	Method        string
	PayloadFormat string
	ContentType   string
	Headers       map[string]string
}

var httpProviderProfiles = map[string]HTTPProviderProfile{
	"sendgrid": {Endpoint: "https://api.sendgrid.com/v3/mail/send", Method: "POST", PayloadFormat: "json", ContentType: "application/json", Headers: map[string]string{"Authorization": "Bearer ${API_KEY}"}},
	"resend":   {Endpoint: "https://api.resend.com/emails", Method: "POST", PayloadFormat: "json", ContentType: "application/json", Headers: map[string]string{"Authorization": "Bearer ${API_KEY}"}},
	"postmark": {Endpoint: "https://api.postmarkapp.com/email", Method: "POST", PayloadFormat: "json", ContentType: "application/json", Headers: map[string]string{"X-Postmark-Server-Token": "${API_KEY}"}},
	"mailgun":  {Endpoint: "https://api.mailgun.net/v3", Method: "POST", PayloadFormat: "form", ContentType: "application/x-www-form-urlencoded", Headers: map[string]string{"Authorization": "Basic ${API_KEY}"}},
}

// emailDomainMap maps email domains to preferred providers (used by inferProvider).
var emailDomainMap = map[string]string{
	"gmail.com":      "gmail",
	"googlemail.com": "gmail",
	"yahoo.com":      "smtp",
}

// RegisterEmailDomainMap lets runtime code add domain->provider mappings.
func RegisterEmailDomainMap(domain, provider string) {
	emailDomainMap[domain] = provider
}

// HTTP client cache and mutex used by getHTTPClient
var (
	httpClientMu    sync.Mutex
	httpClientCache = map[string]*http.Client{}
)

// HTTP payload builder helper types and defaults
type HTTPPayloadBuilder func(cfg *EmailConfig) (any, string, error)

var httpPayloadBuilders = map[string]HTTPPayloadBuilder{}

func init() {
	// default json builder
	httpPayloadBuilders["json"] = func(cfg *EmailConfig) (any, string, error) {
		payload := map[string]any{
			"from":    cfg.From,
			"to":      cfg.To,
			"subject": cfg.Subject,
		}
		if cfg.HTMLBody != "" {
			payload["html"] = cfg.HTMLBody
		}
		if cfg.TextBody != "" {
			payload["text"] = cfg.TextBody
		}
		return payload, "application/json", nil
	}

	// provider-specific builders can be registered into httpPayloadBuilders if needed
}

// buildHTTPPayload builds a generic HTTP payload from the email config when
// no provider-specific builder is available.
func buildHTTPPayload(cfg *EmailConfig) (any, error) {
	payload := map[string]any{
		"from":    cfg.From,
		"to":      cfg.To,
		"subject": cfg.Subject,
	}
	if cfg.HTMLBody != "" {
		payload["html"] = cfg.HTMLBody
	}
	if cfg.TextBody != "" {
		payload["text"] = cfg.TextBody
	}
	// Merge any additional data under "data" key if present
	if len(cfg.AdditionalData) > 0 {
		payload["data"] = cfg.AdditionalData
	}
	return payload, nil
}
