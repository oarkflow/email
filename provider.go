package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Provider defines the interface that all email providers must implement
type Provider interface {
	// Name returns the unique identifier for this provider
	Name() string

	// Transport returns the transport type: "smtp", "http", or custom
	Transport() string

	// BuildPayload constructs the provider-specific payload and returns the
	// payload (usually a map or form), the content type (e.g. "application/json"),
	// and an error if any.
	BuildPayload(cfg *EmailConfig) (payload interface{}, contentType string, err error)

	// GetEndpoint returns the API endpoint for HTTP providers
	GetEndpoint(cfg *EmailConfig) string

	// GetHeaders returns HTTP headers required for authentication
	GetHeaders(cfg *EmailConfig) map[string]string

	// GetSMTPConfig returns SMTP configuration for SMTP providers
	GetSMTPConfig() *SMTPConfig

	// ValidateConfig validates the email configuration for this provider
	ValidateConfig(cfg *EmailConfig) error
}

// SMTPConfig holds SMTP connection details
type SMTPConfig struct {
	Host   string
	Port   int
	UseTLS bool
	UseSSL bool
}

// ProviderMetadata holds additional provider information
type ProviderMetadata struct {
	Capacity    int     // Approximate sends per selection window
	Cost        float64 // Relative cost metric
	Reliability float64 // Reliability score (0-1)
	Priority    int     // Selection priority
}

// ProviderRegistry manages all registered email providers
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	metadata  map[string]ProviderMetadata
	aliases   map[string]string // domain/alias -> provider name
}

var (
	globalRegistry = NewProviderRegistry()
)

// NewProviderRegistry creates a new provider registry
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]Provider),
		metadata:  make(map[string]ProviderMetadata),
		aliases:   make(map[string]string),
	}
}

// Register adds a provider to the registry
func (r *ProviderRegistry) Register(provider Provider, metadata ProviderMetadata) error {
	if provider == nil {
		return errors.New("provider cannot be nil")
	}

	name := strings.ToLower(provider.Name())
	if name == "" {
		return errors.New("provider name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers[name] = provider
	r.metadata[name] = metadata

	return nil
}

// Get retrieves a provider by name
func (r *ProviderRegistry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name = strings.ToLower(name)

	// Check aliases first
	if actual, ok := r.aliases[name]; ok {
		name = actual
	}

	provider, ok := r.providers[name]
	return provider, ok
}

// RegisterAlias creates an alias for a provider
func (r *ProviderRegistry) RegisterAlias(providerName string, alias ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range alias {
		r.aliases[strings.ToLower(a)] = strings.ToLower(providerName)
	}
}

// List returns all registered provider names
func (r *ProviderRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetMetadata retrieves metadata for a provider
func (r *ProviderRegistry) GetMetadata(name string) (ProviderMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metadata, ok := r.metadata[strings.ToLower(name)]
	return metadata, ok
}

// Global registry functions for convenience
func RegisterProvider(provider Provider, metadata ProviderMetadata) error {
	return globalRegistry.Register(provider, metadata)
}

func GetProvider(name string) (Provider, bool) {
	return globalRegistry.Get(name)
}

func RegisterAlias(providerName string, alias ...string) {
	globalRegistry.RegisterAlias(providerName, alias...)
}

func ListProviders() []string {
	return globalRegistry.List()
}

// BaseProvider provides common functionality for all providers
type BaseProvider struct {
	name      string
	transport string
	endpoint  string
	smtp      *SMTPConfig
}

func (b *BaseProvider) Name() string {
	return b.name
}

func (b *BaseProvider) Transport() string {
	return b.transport
}

func (b *BaseProvider) GetEndpoint(cfg *EmailConfig) string {
	if cfg.Endpoint != "" {
		return cfg.Endpoint
	}
	return b.endpoint
}

func (b *BaseProvider) GetSMTPConfig() *SMTPConfig {
	return b.smtp
}

func (b *BaseProvider) ValidateConfig(cfg *EmailConfig) error {
	if cfg.From == "" {
		return errors.New("from address is required")
	}
	if len(cfg.To) == 0 {
		return errors.New("at least one recipient is required")
	}
	if cfg.Subject == "" {
		return errors.New("subject is required")
	}
	return nil
}

// HTTPProvider is a base for HTTP-based providers
type HTTPProvider struct {
	BaseProvider
	headers       map[string]string
	method        string
	contentType   string
	payloadFormat string
}

func NewHTTPProvider(name, endpoint string, headers map[string]string) *HTTPProvider {
	return &HTTPProvider{
		BaseProvider: BaseProvider{
			name:      name,
			transport: "http",
			endpoint:  endpoint,
		},
		headers:     headers,
		method:      http.MethodPost,
		contentType: "application/json",
	}
}

func (h *HTTPProvider) GetHeaders(cfg *EmailConfig) map[string]string {
	headers := make(map[string]string)
	for k, v := range h.headers {
		// Replace placeholder with actual API key from config
		headers[k] = strings.ReplaceAll(v, "${API_KEY}", cfg.APIKey)
	}
	return headers
}

// SMTPProvider is a base for SMTP-based providers
type SMTPProvider struct {
	BaseProvider
}

func NewSMTPProvider(name, host string, port int, useTLS, useSSL bool) *SMTPProvider {
	return &SMTPProvider{
		BaseProvider: BaseProvider{
			name:      name,
			transport: "smtp",
			smtp: &SMTPConfig{
				Host:   host,
				Port:   port,
				UseTLS: useTLS,
				UseSSL: useSSL,
			},
		},
	}
}

func (s *SMTPProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	// SMTP providers use standard MIME message format
	return buildMIMEMessage(cfg)
}

func (s *SMTPProvider) GetHeaders(cfg *EmailConfig) map[string]string {
	return nil // SMTP doesn't use HTTP headers
}

func (s *SMTPProvider) GetEndpoint(cfg *EmailConfig) string {
	return "" // SMTP doesn't use HTTP endpoints
}

// ================= SPECIFIC PROVIDER IMPLEMENTATIONS =================

// SendGridProvider implements SendGrid's API
type SendGridProvider struct {
	*HTTPProvider
}

func NewSendGridProvider() *SendGridProvider {
	return &SendGridProvider{
		HTTPProvider: NewHTTPProvider(
			"sendgrid",
			"https://api.sendgrid.com/v3/mail/send",
			map[string]string{
				"Authorization": "Bearer ${API_KEY}",
			},
		),
	}
}

func (s *SendGridProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	personalization := map[string]interface{}{
		"to": addressMaps(parseAddressList(cfg.To), "email", "name"),
	}
	if len(cfg.CC) > 0 {
		personalization["cc"] = addressMaps(parseAddressList(cfg.CC), "email", "name")
	}
	if len(cfg.BCC) > 0 {
		personalization["bcc"] = addressMaps(parseAddressList(cfg.BCC), "email", "name")
	}
	if cfg.Subject != "" {
		personalization["subject"] = cfg.Subject
	}

	fromName, fromEmail := splitAddress(cfg.From)
	fromEntry := singleAddressMap(simpleAddress{Name: fromName, Email: fromEmail}, "email", "name")

	contents := make([]map[string]string, 0, 2)
	if cfg.TextBody != "" {
		contents = append(contents, map[string]string{"type": "text/plain", "value": cfg.TextBody})
	}
	if cfg.HTMLBody != "" {
		contents = append(contents, map[string]string{"type": "text/html", "value": cfg.HTMLBody})
	}
	if len(contents) == 0 {
		contents = append(contents, map[string]string{"type": "text/plain", "value": fallbackBody(cfg.TextBody)})
	}

	payload := map[string]interface{}{
		"personalizations": []interface{}{personalization},
		"from":             fromEntry,
		"content":          contents,
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["reply_to"] = singleAddressMap(reply, "email", "name")
	}

	if err := s.addAttachments(payload, cfg); err != nil {
		return nil, "", err
	}

	return mergeAdditional(payload, cfg.AdditionalData, true), "application/json", nil
}

func (s *SendGridProvider) addAttachments(payload map[string]interface{}, cfg *EmailConfig) error {
	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]string, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]string{
				"content":  att.Content,
				"type":     att.MIMEType,
				"filename": att.Filename,
			}
			if att.Inline {
				entry["disposition"] = "inline"
				if att.ContentID != "" {
					entry["content_id"] = att.ContentID
				}
			}
			attachments = append(attachments, entry)
		}
		payload["attachments"] = attachments
	}
	return nil
}

// ResendProvider implements Resend's API
type ResendProvider struct {
	*HTTPProvider
}

func NewResendProvider() *ResendProvider {
	return &ResendProvider{
		HTTPProvider: NewHTTPProvider(
			"resend",
			"https://api.resend.com/emails",
			map[string]string{
				"Authorization": "Bearer ${API_KEY}",
			},
		),
	}
}

func (r *ResendProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	payload := map[string]interface{}{
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
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		payload["text"] = fallbackBody(cfg.TextBody)
	}

	if len(cfg.CC) > 0 {
		payload["cc"] = cfg.CC
	}
	if len(cfg.BCC) > 0 {
		payload["bcc"] = cfg.BCC
	}
	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["reply_to"] = reply.Email
	}

	if err := r.addAttachments(payload, cfg); err != nil {
		return nil, "", err
	}

	return mergeAdditional(payload, cfg.AdditionalData, true), "application/json", nil
}

func (r *ResendProvider) addAttachments(payload map[string]interface{}, cfg *EmailConfig) error {
	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]interface{}, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]interface{}{
				"filename": att.Filename,
				"content":  att.Content,
			}
			attachments = append(attachments, entry)
		}
		payload["attachments"] = attachments
	}
	return nil
}

// PostmarkProvider implements Postmark's API
type PostmarkProvider struct {
	*HTTPProvider
}

func NewPostmarkProvider() *PostmarkProvider {
	return &PostmarkProvider{
		HTTPProvider: NewHTTPProvider(
			"postmark",
			"https://api.postmarkapp.com/email",
			map[string]string{
				"X-Postmark-Server-Token": "${API_KEY}",
			},
		),
	}
}

func (p *PostmarkProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	payload := map[string]interface{}{
		"From":    cfg.From,
		"To":      strings.Join(cfg.To, ","),
		"Subject": cfg.Subject,
	}

	if len(cfg.CC) > 0 {
		payload["Cc"] = strings.Join(cfg.CC, ",")
	}
	if len(cfg.BCC) > 0 {
		payload["Bcc"] = strings.Join(cfg.BCC, ",")
	}
	if cfg.TextBody != "" {
		payload["TextBody"] = cfg.TextBody
	}
	if cfg.HTMLBody != "" {
		payload["HtmlBody"] = cfg.HTMLBody
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		payload["TextBody"] = fallbackBody(cfg.TextBody)
	}
	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["ReplyTo"] = reply.Email
	}

	if err := p.addAttachments(payload, cfg); err != nil {
		return nil, "", err
	}

	return payload, "application/json", nil
}

func (p *PostmarkProvider) addAttachments(payload map[string]interface{}, cfg *EmailConfig) error {
	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]string, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]string{
				"Name":        att.Filename,
				"Content":     att.Content,
				"ContentType": att.MIMEType,
			}
			if att.ContentID != "" {
				entry["ContentID"] = att.ContentID
			}
			attachments = append(attachments, entry)
		}
		payload["Attachments"] = attachments
	}
	return nil
}

// MailgunProvider implements Mailgun's API
type MailgunProvider struct {
	*HTTPProvider
}

func NewMailgunProvider() *MailgunProvider {
	return &MailgunProvider{
		HTTPProvider: &HTTPProvider{
			BaseProvider: BaseProvider{
				name:      "mailgun",
				transport: "http",
				endpoint:  "https://api.mailgun.net/v3",
			},
			headers: map[string]string{
				"Authorization": "Basic ${API_KEY}",
			},
			method:      http.MethodPost,
			contentType: "application/x-www-form-urlencoded",
		},
	}
}

func (m *MailgunProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	domain := m.extractDomain(cfg)
	if domain == "" {
		return nil, "", errors.New("mailgun domain is required")
	}

	form := url.Values{}
	fromAddr := cfg.From
	if cfg.FromName != "" {
		fromAddr = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.From)
	}
	form.Set("from", fromAddr)

	for _, to := range cfg.To {
		form.Add("to", to)
	}
	for _, cc := range cfg.CC {
		form.Add("cc", cc)
	}
	for _, bcc := range cfg.BCC {
		form.Add("bcc", bcc)
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		form.Set("h:Reply-To", reply.Email)
	}

	form.Set("subject", cfg.Subject)
	if cfg.TextBody != "" {
		form.Set("text", cfg.TextBody)
	}
	if cfg.HTMLBody != "" {
		form.Set("html", cfg.HTMLBody)
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		form.Set("text", fallbackBody(cfg.TextBody))
	}

	return form, "application/x-www-form-urlencoded", nil
}

func (m *MailgunProvider) extractDomain(cfg *EmailConfig) string {
	// Try to get from config
	domain := strings.TrimSpace(firstString(cfg.AdditionalData, "domain", "mailgun_domain"))
	if domain != "" {
		return domain
	}

	// Try to infer from endpoint
	if cfg.Endpoint != "" {
		domain = inferMailgunDomain(cfg.Endpoint)
		if domain != "" {
			return domain
		}
	}

	// Extract from sender address
	parts := strings.Split(cfg.From, "@")
	if len(parts) == 2 {
		return parts[1]
	}

	return ""
}

func (m *MailgunProvider) GetEndpoint(cfg *EmailConfig) string {
	domain := m.extractDomain(cfg)
	if cfg.Endpoint != "" && !strings.Contains(cfg.Endpoint, "/messages") {
		return strings.TrimRight(cfg.Endpoint, "/") + "/" + domain + "/messages"
	}
	return m.endpoint + "/" + domain + "/messages"
}

// AWSProvider implements AWS SES V2 API
type AWSProvider struct {
	*HTTPProvider
}

func NewAWSProvider() *AWSProvider {
	return &AWSProvider{
		HTTPProvider: NewHTTPProvider(
			"aws_ses",
			"https://email.us-east-1.amazonaws.com/v2/email/outbound-emails",
			map[string]string{},
		),
	}
}

func (a *AWSProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	raw, err := buildMessage(cfg)
	if err != nil {
		return nil, "", err
	}

	dest := map[string][]string{}
	if len(cfg.To) > 0 {
		dest["ToAddresses"] = cfg.To
	}
	if len(cfg.CC) > 0 {
		dest["CcAddresses"] = cfg.CC
	}
	if len(cfg.BCC) > 0 {
		dest["BccAddresses"] = cfg.BCC
	}

	payload := map[string]interface{}{
		"Content": map[string]interface{}{
			"Raw": map[string]string{
				"Data": base64.StdEncoding.EncodeToString([]byte(raw)),
			},
		},
	}

	if len(dest) > 0 {
		payload["Destination"] = dest
	}
	if cfg.From != "" {
		payload["FromEmailAddress"] = cfg.From
	}
	if cfg.ConfigurationSet != "" {
		payload["ConfigurationSetName"] = cfg.ConfigurationSet
	}

	if len(cfg.Tags) > 0 {
		tags := make([]map[string]string, 0, len(cfg.Tags))
		for k, v := range cfg.Tags {
			tags = append(tags, map[string]string{"Name": k, "Value": v})
		}
		sort.Slice(tags, func(i, j int) bool { return tags[i]["Name"] < tags[j]["Name"] })
		payload["EmailTags"] = tags
	}

	return payload, "application/json", nil
}

// ProviderFactory creates providers from configuration
type ProviderFactory struct {
	constructors map[string]func() Provider
}

var defaultFactory = &ProviderFactory{
	constructors: map[string]func() Provider{},
}

func (f *ProviderFactory) Register(name string, constructor func() Provider) {
	f.constructors[strings.ToLower(name)] = constructor
}

func (f *ProviderFactory) Create(name string) (Provider, error) {
	constructor, ok := f.constructors[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return constructor(), nil
}

// RegisterProviderConstructor registers a provider constructor
func RegisterProviderConstructor(name string, constructor func() Provider) {
	defaultFactory.Register(name, constructor)
}

// CreateProvider creates a provider instance
func CreateProvider(name string) (Provider, error) {
	return defaultFactory.Create(name)
}

// Initialize default providers
func init() {
	// Register HTTP providers
	RegisterProvider(NewSendGridProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.5, Reliability: 0.99})
	RegisterProvider(NewResendProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.3, Reliability: 0.98})
	RegisterProvider(NewPostmarkProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.4, Reliability: 0.99})
	RegisterProvider(NewMailgunProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.4, Reliability: 0.98})
	RegisterProvider(NewAWSProvider(), ProviderMetadata{Capacity: 5000, Cost: 0.1, Reliability: 0.99})

	// Register SMTP providers
	RegisterProvider(NewSMTPProvider("gmail", "smtp.gmail.com", 587, true, false),
		ProviderMetadata{Capacity: 500, Cost: 0.0, Reliability: 0.95})
	RegisterProvider(NewSMTPProvider("outlook", "smtp-mail.outlook.com", 587, true, false),
		ProviderMetadata{Capacity: 500, Cost: 0.0, Reliability: 0.95})

	// Register aliases
	RegisterAlias("sendgrid", "sendgrid")
	RegisterAlias("aws_ses", "ses", "amazon_ses")
	RegisterAlias("gmail", "google", "gmail_smtp")
	// Add a local MailHog SMTP provider (useful for local development & testing)
	RegisterProvider(NewSMTPProvider("mailhog", "localhost", 1025, false, false), ProviderMetadata{Capacity: 0, Cost: 0.0, Reliability: 0.99})
	RegisterAlias("mailhog", "mailhog")
}

func inferMailgunDomain(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, segment := range segments {
		if strings.EqualFold(segment, "v3") && i+1 < len(segments) {
			return segments[i+1]
		}
	}
	return ""
}

type EncodedAttachment struct {
	Filename  string
	Content   string
	MIMEType  string
	Inline    bool
	ContentID string
}

func buildMIMEMessage(cfg *EmailConfig) (interface{}, string, error) {
	msg, err := buildMessage(cfg)
	return msg, "message/rfc822", err
}
