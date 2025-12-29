package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============= EXAMPLE: Adding New Providers =============

// BrevoProvider (Sendinblue) implementation
type BrevoProvider struct {
	*HTTPProvider
}

func NewBrevoProvider() *BrevoProvider {
	return &BrevoProvider{
		HTTPProvider: NewHTTPProvider(
			"brevo",
			"https://api.brevo.com/v3/smtp/email",
			map[string]string{
				"accept":  "application/json",
				"api-key": "${API_KEY}",
			},
		),
	}
}

func (b *BrevoProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)
	sender := singleAddressMap(simpleAddress{Name: fromName, Email: fromEmail}, "email", "name")

	payload := map[string]interface{}{
		"sender":  sender,
		"to":      addressMaps(parseAddressList(cfg.To), "email", "name"),
		"subject": cfg.Subject,
	}

	if cfg.HTMLBody != "" {
		payload["htmlContent"] = cfg.HTMLBody
	}
	if cfg.TextBody != "" {
		payload["textContent"] = cfg.TextBody
	}

	if len(cfg.CC) > 0 {
		payload["cc"] = addressMaps(parseAddressList(cfg.CC), "email", "name")
	}
	if len(cfg.BCC) > 0 {
		payload["bcc"] = addressMaps(parseAddressList(cfg.BCC), "email", "name")
	}

	return mergeAdditional(payload, cfg.AdditionalData, true), "application/json", nil
}

// MailjetProvider implementation
type MailjetProvider struct {
	*HTTPProvider
}

func NewMailjetProvider() *MailjetProvider {
	return &MailjetProvider{
		HTTPProvider: NewHTTPProvider(
			"mailjet",
			"https://api.mailjet.com/v3.1/send",
			map[string]string{
				"Authorization": "Basic ${API_KEY}",
			},
		),
	}
}

func (m *MailjetProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)

	toList := make([]map[string]string, 0, len(cfg.To))
	for _, addr := range parseAddressList(cfg.To) {
		toList = append(toList, map[string]string{"Email": addr.Email, "Name": addr.Name})
	}

	message := map[string]interface{}{
		"From": map[string]string{
			"Email": fromEmail,
			"Name":  fromName,
		},
		"To":      toList,
		"Subject": cfg.Subject,
	}

	if cfg.TextBody != "" {
		message["TextPart"] = cfg.TextBody
	}
	if cfg.HTMLBody != "" {
		message["HTMLPart"] = cfg.HTMLBody
	}

	payload := map[string]interface{}{
		"Messages": []interface{}{message},
	}

	return payload, "application/json", nil
}

// SparkPostProvider implementation
type SparkPostProvider struct {
	*HTTPProvider
}

func NewSparkPostProvider() *SparkPostProvider {
	return &SparkPostProvider{
		HTTPProvider: NewHTTPProvider(
			"sparkpost",
			"https://api.sparkpost.com/api/v1/transmissions",
			map[string]string{
				"Authorization": "${API_KEY}",
			},
		),
	}
}

func (s *SparkPostProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)

	content := map[string]interface{}{
		"from":    map[string]string{"email": fromEmail, "name": fromName},
		"subject": cfg.Subject,
	}

	if cfg.HTMLBody != "" {
		content["html"] = cfg.HTMLBody
	}
	if cfg.TextBody != "" {
		content["text"] = cfg.TextBody
	}

	recipients := make([]map[string]interface{}, 0, len(cfg.To))
	for _, addr := range cfg.To {
		recipients = append(recipients, map[string]interface{}{
			"address": map[string]string{"email": strings.TrimSpace(addr)},
		})
	}

	payload := map[string]interface{}{
		"recipients": recipients,
		"content":    content,
	}

	return payload, "application/json", nil
}

// MailtrapProvider implementation
type MailtrapProvider struct {
	*HTTPProvider
}

func NewMailtrapProvider() *MailtrapProvider {
	return &MailtrapProvider{
		HTTPProvider: NewHTTPProvider(
			"mailtrap",
			"https://send.api.mailtrap.io/api/send",
			map[string]string{
				"Api-Token": "${API_KEY}",
			},
		),
	}
}

func (m *MailtrapProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)
	sender := singleAddressMap(simpleAddress{Name: fromName, Email: fromEmail}, "email", "name")

	payload := map[string]interface{}{
		"from":    sender,
		"to":      addressMaps(parseAddressList(cfg.To), "email", "name"),
		"subject": cfg.Subject,
	}

	if cfg.TextBody != "" {
		payload["text"] = cfg.TextBody
	}
	if cfg.HTMLBody != "" {
		payload["html"] = cfg.HTMLBody
	}

	return payload, "application/json", nil
}

// ============= GENERIC PROVIDER BUILDER =============

// GenericJSONProvider allows creating providers via configuration
type GenericJSONProvider struct {
	*HTTPProvider
	transformer PayloadTransformer
}

// PayloadTransformer defines how to transform EmailConfig to provider payload
type PayloadTransformer interface {
	Transform(cfg *EmailConfig) (map[string]interface{}, error)
}

// JSONMapping defines field mappings from EmailConfig to provider format
type JSONMapping struct {
	From        string            // Field name for "from"
	To          string            // Field name for "to"
	Subject     string            // Field name for "subject"
	TextBody    string            // Field name for text body
	HTMLBody    string            // Field name for HTML body
	CC          string            // Field name for CC
	BCC         string            // Field name for BCC
	ReplyTo     string            // Field name for reply-to
	Custom      map[string]string // Custom field mappings
	Nested      map[string]string // Nested structure mappings
	ToArray     bool              // Whether "to" should be array of strings or objects
	AddressType string            // "simple" or "object" for address format
}

// MappingTransformer implements PayloadTransformer using field mappings
type MappingTransformer struct {
	Mapping JSONMapping
}

func (m *MappingTransformer) Transform(cfg *EmailConfig) (map[string]interface{}, error) {
	payload := make(map[string]interface{})

	// Map basic fields
	if m.Mapping.From != "" {
		payload[m.Mapping.From] = cfg.From
	}
	if m.Mapping.To != "" {
		if m.Mapping.ToArray {
			payload[m.Mapping.To] = cfg.To
		} else {
			payload[m.Mapping.To] = addressMaps(parseAddressList(cfg.To), "email", "name")
		}
	}
	if m.Mapping.Subject != "" {
		payload[m.Mapping.Subject] = cfg.Subject
	}
	if m.Mapping.TextBody != "" && cfg.TextBody != "" {
		payload[m.Mapping.TextBody] = cfg.TextBody
	}
	if m.Mapping.HTMLBody != "" && cfg.HTMLBody != "" {
		payload[m.Mapping.HTMLBody] = cfg.HTMLBody
	}
	if m.Mapping.CC != "" && len(cfg.CC) > 0 {
		payload[m.Mapping.CC] = cfg.CC
	}
	if m.Mapping.BCC != "" && len(cfg.BCC) > 0 {
		payload[m.Mapping.BCC] = cfg.BCC
	}

	// Apply custom mappings
	for configKey, payloadKey := range m.Mapping.Custom {
		if val, ok := cfg.AdditionalData[configKey]; ok {
			payload[payloadKey] = val
		}
	}

	return payload, nil
}

func NewGenericJSONProvider(name, endpoint string, headers map[string]string, mapping JSONMapping) *GenericJSONProvider {
	return &GenericJSONProvider{
		HTTPProvider: NewHTTPProvider(name, endpoint, headers),
		transformer:  &MappingTransformer{Mapping: mapping},
	}
}

func (g *GenericJSONProvider) BuildPayload(cfg *EmailConfig) (interface{}, string, error) {
	payload, err := g.transformer.Transform(cfg)
	if err != nil {
		return nil, "", err
	}
	return payload, "application/json", nil
}

// ============= PROVIDER LOADER FROM CONFIG =============

// ProviderConfig represents a provider configuration from JSON/YAML
type ProviderConfig struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"` // "http", "smtp", "generic"
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
	Mapping  *JSONMapping      `json:"mapping,omitempty"`
	SMTP     *SMTPConfig       `json:"smtp,omitempty"`
	Metadata ProviderMetadata  `json:"metadata"`
}

// LoadProviderFromConfig creates a provider from configuration
func LoadProviderFromConfig(config ProviderConfig) (Provider, error) {
	switch config.Type {
	case "smtp":
		if config.SMTP == nil {
			return nil, fmt.Errorf("SMTP config required for SMTP provider")
		}
		return NewSMTPProvider(
			config.Name,
			config.SMTP.Host,
			config.SMTP.Port,
			config.SMTP.UseTLS,
			config.SMTP.UseSSL,
		), nil

	case "generic", "http":
		if config.Mapping == nil {
			return nil, fmt.Errorf("mapping required for generic provider")
		}
		return NewGenericJSONProvider(
			config.Name,
			config.Endpoint,
			config.Headers,
			*config.Mapping,
		), nil

	default:
		return nil, fmt.Errorf("unknown provider type: %s", config.Type)
	}
}

// LoadProvidersFromJSON loads multiple providers from JSON configuration
func LoadProvidersFromJSON(jsonData []byte) error {
	var configs []ProviderConfig
	if err := json.Unmarshal(jsonData, &configs); err != nil {
		return err
	}

	for _, config := range configs {
		provider, err := LoadProviderFromConfig(config)
		if err != nil {
			return fmt.Errorf("failed to load provider %s: %w", config.Name, err)
		}

		if err := RegisterProvider(provider, config.Metadata); err != nil {
			return fmt.Errorf("failed to register provider %s: %w", config.Name, err)
		}
	}

	return nil
}

// ============= USAGE EXAMPLES =============

// Example 1: Registering a custom provider programmatically
func ExampleRegisterCustomProvider() {
	// Create and register a new provider
	customProvider := NewBrevoProvider()
	metadata := ProviderMetadata{
		Capacity:    1000,
		Cost:        0.4,
		Reliability: 0.98,
		Priority:    10,
	}

	if err := RegisterProvider(customProvider, metadata); err != nil {
		fmt.Printf("Failed to register provider: %v\n", err)
		return
	}

	// Register aliases
	RegisterAlias("sendinblue", "brevo")

	fmt.Println("Provider registered successfully")
}

// Example 2: Loading providers from JSON configuration
func ExampleLoadProvidersFromJSON() {
	configJSON := `[
		{
			"name": "custom_smtp",
			"type": "smtp",
			"smtp": {
				"Host": "smtp.example.com",
				"Port": 587,
				"UseTLS": true,
				"UseSSL": false
			},
			"metadata": {
				"capacity": 500,
				"cost": 0.2,
				"reliability": 0.95,
				"priority": 5
			}
		},
		{
			"name": "custom_api",
			"type": "generic",
			"endpoint": "https://api.example.com/v1/send",
			"headers": {
				"Authorization": "Bearer ${API_KEY}",
				"Content-Type": "application/json"
			},
			"mapping": {
				"From": "sender",
				"To": "recipients",
				"Subject": "subject",
				"TextBody": "text",
				"HTMLBody": "html",
				"ToArray": true
			},
			"metadata": {
				"capacity": 1000,
				"cost": 0.3,
				"reliability": 0.97,
				"priority": 8
			}
		}
	]`

	if err := LoadProvidersFromJSON([]byte(configJSON)); err != nil {
		fmt.Printf("Failed to load providers: %v\n", err)
		return
	}

	fmt.Println("Providers loaded from JSON successfully")
}

// Example 3: Using a provider
func ExampleUseProvider() {
	// Get a provider
	provider, ok := GetProvider("sendgrid")
	if !ok {
		fmt.Println("Provider not found")
		return
	}

	// Create email configuration
	cfg := &EmailConfig{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Subject:  "Test Email",
		TextBody: "Hello, World!",
		HTMLBody: "<h1>Hello, World!</h1>",
		APIKey:   "your-api-key",
	}

	// Validate configuration
	if err := provider.ValidateConfig(cfg); err != nil {
		fmt.Printf("Invalid configuration: %v\n", err)
		return
	}

	// Build payload
	payload, contentType, err := provider.BuildPayload(cfg)
	if err != nil {
		fmt.Printf("Failed to build payload: %v\n", err)
		return
	}

	// Get endpoint and headers
	endpoint := provider.GetEndpoint(cfg)
	headers := provider.GetHeaders(cfg)

	fmt.Printf("Endpoint: %s\n", endpoint)
	fmt.Printf("Content-Type: %s\n", contentType)
	fmt.Printf("Headers: %v\n", headers)
	fmt.Printf("Payload: %v\n", payload)
}

// Example 4: Creating a custom transformer
type CustomTransformer struct {
	// Custom logic here
}

func (c *CustomTransformer) Transform(cfg *EmailConfig) (map[string]interface{}, error) {
	// Implement custom transformation logic
	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"from":    cfg.From,
			"to":      cfg.To,
			"subject": cfg.Subject,
			"body": map[string]string{
				"text": cfg.TextBody,
				"html": cfg.HTMLBody,
			},
		},
		"metadata": cfg.Tags,
	}
	return payload, nil
}

// Example 5: Provider selection strategy
type ProviderSelector interface {
	Select(cfg *EmailConfig, providers []string) (string, error)
}

type CostBasedSelector struct{}

func (s *CostBasedSelector) Select(cfg *EmailConfig, providers []string) (string, error) {
	var bestProvider string
	lowestCost := float64(999999)

	for _, name := range providers {
		metadata, ok := globalRegistry.GetMetadata(name)
		if !ok {
			continue
		}

		// Select based on cost and reliability
		score := metadata.Cost / metadata.Reliability
		if score < lowestCost {
			lowestCost = score
			bestProvider = name
		}
	}

	if bestProvider == "" {
		return "", fmt.Errorf("no suitable provider found")
	}

	return bestProvider, nil
}

// Initialize extension providers
func InitExtensionProviders() {
	// Register additional providers
	RegisterProvider(NewBrevoProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.4, Reliability: 0.98})
	RegisterProvider(NewMailjetProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.45, Reliability: 0.97})
	RegisterProvider(NewSparkPostProvider(), ProviderMetadata{Capacity: 1000, Cost: 0.4, Reliability: 0.98})
	RegisterProvider(NewMailtrapProvider(), ProviderMetadata{Capacity: 100, Cost: 0.0, Reliability: 0.99})

	// Register aliases
	RegisterAlias("sendinblue", "brevo")
}
