package main

import (
	"bytes"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EmailConfig represents the fully normalized configuration.
type EmailConfig struct {
	From                string
	FromName            string
	EnvelopeFrom        string
	ReturnPath          string
	ReplyTo             []string
	To                  []string
	CC                  []string
	BCC                 []string
	ListUnsubscribe     []string
	ListUnsubscribePost bool
	Subject             string
	Body                string
	TextBody            string
	HTMLBody            string
	Attachments         []Attachment
	ConfigurationSet    string
	Tags                map[string]string
	Provider            string
	Transport           string
	Host                string
	Port                int
	Username            string
	Password            string
	APIKey              string
	APIToken            string
	Endpoint            string
	HTTPMethod          string
	Headers             map[string]string
	QueryParams         map[string]string
	HTTPPayload         map[string]any
	PayloadFormat       string
	HTTPContentType     string
	HTTPAuth            string
	HTTPAuthHeader      string
	HTTPAuthQuery       string
	HTTPAuthPrefix      string
	MaxConnsPerHost     int
	MaxIdleConns        int
	MaxIdleConnsHost    int
	DisableKeepAlives   bool
	SMTPAuth            string
	HTMLTemplatePath    string
	TextTemplatePath    string
	BodyTemplatePath    string
	AdditionalData      map[string]any
	ScheduleMode        string
	RawSubject          string         `json:"-"`
	RawBody             string         `json:"-"`
	RawTextBody         string         `json:"-"`
	RawHTMLBody         string         `json:"-"`
	RawHTTPPayload      map[string]any `json:"-"`
	AWSRegion           string
	AWSAccessKey        string
	AWSSecretKey        string
	AWSSessionToken     string
	UseTLS              bool
	UseSSL              bool
	SkipTLSVerify       bool
	Timeout             time.Duration
	RetryCount          int
	RetryDelay          time.Duration
	// MaxRetryDelay caps exponential backoff delay (optional).
	MaxRetryDelay time.Duration
	// ProviderPriority is an ordered list of provider names to attempt in case of failures.
	ProviderPriority []string
}

// Attachment describes a file to be included with the email.
type Attachment struct {
	Source    string
	Name      string
	MIMEType  string
	Inline    bool
	ContentID string
}

type encodedAttachment struct {
	Filename  string
	MIMEType  string
	Content   string
	Inline    bool
	ContentID string
}

var fieldAliases = map[string][]string{
	"from":                    {"from", "sender", "from_email", "fromaddress", "sender_email", "mailfrom"},
	"from_name":               {"from_name", "sender_name", "fromname", "display_name", "name"},
	"return_path":             {"return_path", "bounce", "envelope_from", "returnpath"},
	"envelope_from":           {"envelope_from", "mail_from", "mfrom"},
	"reply_to":                {"reply_to", "replyto", "respond_to", "response_to"},
	"to":                      {"to", "recipient", "recipients", "send_to", "sending_to", "mail_to", "to_email", "sendto"},
	"cc":                      {"cc", "carbon_copy", "copy_to"},
	"bcc":                     {"bcc", "blind_carbon_copy", "blind_copy"},
	"list_unsubscribe":        {"list_unsubscribe", "unsubscribe", "listunsubscribe"},
	"list_unsubscribe_post":   {"list_unsubscribe_post", "unsubscribe_post", "one_click"},
	"subject":                 {"subject", "title", "email_subject"},
	"body":                    {"body", "message", "msg", "content", "email_content", "text"},
	"body_html":               {"body_html", "html_body", "html", "message_html"},
	"body_text":               {"body_text", "text_body", "plain_text", "message_text"},
	"attachments":             {"attachments", "attachment", "files", "file", "attach"},
	"configuration_set":       {"configuration_set", "config_set", "ses_configuration_set"},
	"tags":                    {"tags", "ses_tags", "metadata", "ses_metadata"},
	"provider":                {"provider", "use", "service", "email_service"},
	"type":                    {"type", "transport", "channel", "method"},
	"host":                    {"host", "server", "smtp_host", "address", "addr", "smtp_server"},
	"port":                    {"port", "smtp_port"},
	"username":                {"username", "user", "email", "login", "auth_user"},
	"password":                {"password", "pass", "pwd", "auth_password"},
	"api_key":                 {"api_key", "apikey", "key"},
	"api_token":               {"api_token", "apitoken", "token", "access_token", "bearer", "bearer_token"},
	"endpoint":                {"endpoint", "url", "api_url", "api_endpoint"},
	"http_method":             {"http_method", "httpverb", "method"},
	"headers":                 {"headers", "custom_headers", "http_headers"},
	"query_params":            {"query_params", "query", "params", "querystrings", "querystring"},
	"http_payload":            {"http_payload", "payload", "http_body", "custom_payload"},
	"payload_format":          {"payload_format", "http_profile", "http_format"},
	"http_content_type":       {"http_content_type", "payload_content_type", "http_payload_type"},
	"http_auth":               {"http_auth", "auth", "auth_type"},
	"http_auth_header":        {"http_auth_header", "auth_header", "api_key_header"},
	"http_auth_query":         {"http_auth_query", "auth_query", "api_key_query", "auth_param"},
	"http_auth_prefix":        {"http_auth_prefix", "auth_prefix", "bearer_prefix"},
	"schedule_mode":           {"schedule_mode", "schedule"},
	"max_conns_per_host":      {"max_conns_per_host", "max_connections", "max_conns"},
	"max_idle_conns":          {"max_idle_conns", "idle_conns", "max_idle"},
	"max_idle_conns_per_host": {"max_idle_conns_per_host", "max_idle_host", "idle_conns_host"},
	"disable_keepalives":      {"disable_keepalives", "no_keepalive", "disable_keep_alive"},
	"smtp_auth":               {"smtp_auth", "smtp_auth_type", "smtp_auth_mechanism"},
	"html_template":           {"html_template", "template_html", "html_file", "html_path"},
	"text_template":           {"text_template", "template_text", "text_file", "text_path"},
	"body_template":           {"body_template", "message_template", "msg_template", "message_file", "template_message"},
	"timeout":                 {"timeout", "timeout_seconds", "request_timeout", "http_timeout"},
	"retries":                 {"retries", "retry", "retry_count", "attempts"},
	"retry_delay":             {"retry_delay", "retry_wait", "retry_backoff", "retry_pause"},
	"use_tls":                 {"use_tls", "tls", "starttls", "enable_tls"},
	"use_ssl":                 {"use_ssl", "ssl", "enable_ssl"},
	"skip_tls_verify":         {"skip_tls_verify", "insecure", "disable_tls_verify"},
	"aws_region":              {"aws_region", "region"},
	"aws_access_key":          {"aws_access_key", "access_key", "aws_access_key_id"},
	"aws_secret_key":          {"aws_secret_key", "secret_key", "aws_secret_access_key"},
	"aws_session_token":       {"aws_session_token", "session_token", "aws_token"},
}

func init() {
	log.SetFlags(0)
	mrand.Seed(time.Now().UnixNano())
	for canonical, aliases := range fieldAliases {
		seen := make(map[string]struct{})
		normalized := make([]string, 0, len(aliases)+1)
		for _, alias := range aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			lower := strings.ToLower(alias)
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			normalized = append(normalized, alias)
		}
		if _, ok := seen[strings.ToLower(canonical)]; !ok {
			normalized = append(normalized, canonical)
		}
		fieldAliases[canonical] = normalized
	}
}

func main() {
	templatePath := flag.String("template", "", "path to the template JSON file (base config)")
	payloadPath := flag.String("payload", "", "path to the payload JSON file (overrides/template data)")
	worker := flag.Bool("worker", false, "start scheduler worker")
	storePath := flag.String("store", "scheduler_store.json", "path to scheduler store file")
	schedule := flag.Bool("schedule", false, "schedule this email instead of sending now")
	flag.Parse()

	// If the user only asked to run the worker, start it immediately (no template required).
	if *worker {
		store := NewFileJobStore(*storePath)
		s := NewScheduler(store, 5*time.Second)
		if err := s.Start(); err != nil {
			log.Fatalf("cannot start scheduler: %v", err)
		}
		// block forever; in a real system you'd integrate graceful shutdown
		select {}
	}

	raw, err := loadConfigFiles(*templatePath, *payloadPath, flag.Args())
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	config, err := parseConfig(raw)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// If user explicitly asked to schedule, do so
	if *schedule {
		store := NewFileJobStore(*storePath)
		s := NewScheduler(store, 5*time.Second)
		// if user requested a workflow, schedule the defined sequence
		// - named "welcome" (legacy)
		if wf, ok := config.AdditionalData["workflow"].(string); ok && wf == "welcome" {
			if err := ScheduleWelcomeWorkflow(s, config); err != nil {
				log.Fatalf("schedule workflow failed: %v", err)
			}
			return
		}
		// - custom workflow passed as "workflow_steps" or "workflow_definition" (array of steps)
		if def, ok := config.AdditionalData["workflow_steps"]; ok {
			if err := ScheduleGenericWorkflow(s, config, def); err != nil {
				log.Fatalf("schedule workflow failed: %v", err)
			}
			return
		}
		if def, ok := config.AdditionalData["workflow_definition"]; ok {
			if err := ScheduleGenericWorkflow(s, config, def); err != nil {
				log.Fatalf("schedule workflow failed: %v", err)
			}
			return
		}
		// also allow ``workflow`` to be an inline array of steps
		if arr, ok := config.AdditionalData["workflow"].([]any); ok {
			if err := ScheduleGenericWorkflow(s, config, arr); err != nil {
				log.Fatalf("schedule workflow failed: %v", err)
			}
			return
		}

		runAt := time.Now()
		if v, ok := config.AdditionalData["run_at"].(string); ok && v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				runAt = t
			}
		} else if d, ok := config.AdditionalData["delay_seconds"].(float64); ok && d > 0 {
			runAt = time.Now().Add(time.Duration(d) * time.Second)
		}
		job, err := s.Schedule(config, runAt, nil)
		if err != nil {
			log.Fatalf("schedule failed: %v", err)
		}
		log.Printf("scheduled job %s to run at %s", job.ID, job.RunAt)
		return
	}

	log.Printf("Sending email to %v via %s (%s)...", config.To, config.TransportDetails(), config.ProviderOrHost())
	if err := sendEmail(config, nil); err != nil {
		if errors.Is(err, errDeduplicated) {
			log.Println("Send skipped: duplicate detected (schedule=once)")
			return
		}
		log.Fatalf("send failed: %v", err)
	}
	log.Println("Email sent successfully!")
}

func loadConfigFiles(templateFlag, payloadFlag string, args []string) (map[string]any, error) {
	templatePath := templateFlag
	remaining := args
	if templatePath == "" {
		if len(remaining) == 0 {
			printUsage()
			return nil, errors.New("no template or config file provided")
		}
		templatePath = remaining[0]
		remaining = remaining[1:]
	}
	payloadPath := payloadFlag
	if payloadPath == "" && len(remaining) > 0 {
		payloadPath = remaining[0]
	}

	base, err := readJSONFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", templatePath, err)
	}
	log.Printf("Loaded template: %s", templatePath)
	if payloadPath == "" {
		return base, nil
	}
	override, err := readJSONFile(payloadPath)
	if err != nil {
		return nil, fmt.Errorf("payload %s: %w", payloadPath, err)
	}
	log.Printf("Applying payload overrides: %s", payloadPath)
	return mergeConfigMaps(base, override), nil
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run main.go <config.json>")
	fmt.Println("  go run main.go --template template.json --payload payload.json")
	fmt.Println("  go run main.go template.json payload.json")
	fmt.Println("\nExamples:\n  go run main.go config.json\n  go run main.go --template template.smtp.json --payload payload.release.json")
}

func parseConfig(raw map[string]any) (*EmailConfig, error) {
	norm := newNormalizedConfig(raw)
	cfg := &EmailConfig{
		Headers:     map[string]string{},
		QueryParams: map[string]string{},
	}

	cfg.From = getStringField(norm, "from")
	cfg.FromName = getStringField(norm, "from_name")
	cfg.ReturnPath = getStringField(norm, "return_path")
	if env := getStringField(norm, "envelope_from"); env != "" {
		cfg.EnvelopeFrom = env
	}
	cfg.ReplyTo = getStringArrayField(norm, "reply_to")
	cfg.To = getStringArrayField(norm, "to")
	cfg.CC = getStringArrayField(norm, "cc")
	cfg.BCC = getStringArrayField(norm, "bcc")
	cfg.ListUnsubscribe = getStringArrayField(norm, "list_unsubscribe")
	cfg.ListUnsubscribePost = getBoolField(norm, "list_unsubscribe_post")
	cfg.Subject = getStringField(norm, "subject")
	cfg.Body = getStringField(norm, "body")
	cfg.TextBody = getStringField(norm, "body_text")
	cfg.HTMLBody = getStringField(norm, "body_html")
	cfg.HTMLTemplatePath = getStringField(norm, "html_template")
	cfg.TextTemplatePath = getStringField(norm, "text_template")
	cfg.BodyTemplatePath = getStringField(norm, "body_template")
	cfg.ConfigurationSet = getStringField(norm, "configuration_set")
	cfg.Tags = getStringMapField(norm, "tags")

	attachments, err := getAttachments(norm, "attachments")
	if err != nil {
		return nil, err
	}
	cfg.Attachments = attachments

	cfg.Provider = strings.ToLower(getStringField(norm, "provider"))
	cfg.Transport = strings.ToLower(getStringField(norm, "type"))
	cfg.Host = getStringField(norm, "host")
	cfg.Port = getIntField(norm, "port")
	cfg.Username = getStringField(norm, "username")
	cfg.Password = getStringField(norm, "password")
	cfg.APIKey = getStringField(norm, "api_key")
	cfg.APIToken = getStringField(norm, "api_token")
	cfg.Endpoint = getStringField(norm, "endpoint")
	cfg.HTTPMethod = strings.ToUpper(getStringField(norm, "http_method"))
	if cfg.HTTPMethod == "" {
		cfg.HTTPMethod = http.MethodPost
	}
	cfg.Headers = ensureStringMap(getStringMapField(norm, "headers"))
	cfg.QueryParams = ensureStringMap(getStringMapField(norm, "query_params"))
	cfg.HTTPPayload = getObjectField(norm, "http_payload")
	cfg.PayloadFormat = strings.ToLower(getStringField(norm, "payload_format"))
	cfg.HTTPContentType = getStringField(norm, "http_content_type")
	cfg.HTTPAuth = strings.ToLower(getStringField(norm, "http_auth"))
	cfg.HTTPAuthHeader = getStringField(norm, "http_auth_header")
	cfg.HTTPAuthQuery = getStringField(norm, "http_auth_query")
	cfg.HTTPAuthPrefix = getStringField(norm, "http_auth_prefix")
	cfg.MaxConnsPerHost = getIntField(norm, "max_conns_per_host")
	cfg.MaxIdleConns = getIntField(norm, "max_idle_conns")
	cfg.MaxIdleConnsHost = getIntField(norm, "max_idle_conns_per_host")
	cfg.DisableKeepAlives = getBoolField(norm, "disable_keepalives")
	cfg.SMTPAuth = strings.ToLower(getStringField(norm, "smtp_auth"))
	cfg.AWSRegion = getStringField(norm, "aws_region")
	cfg.AWSAccessKey = getStringField(norm, "aws_access_key")
	cfg.AWSSecretKey = getStringField(norm, "aws_secret_key")
	cfg.AWSSessionToken = getStringField(norm, "aws_session_token")
	cfg.Timeout = getDurationField(norm, "timeout")
	cfg.RetryCount = getIntField(norm, "retries")
	cfg.RetryDelay = getDurationField(norm, "retry_delay")
	cfg.MaxRetryDelay = getDurationField(norm, "max_retry_delay")
	cfg.ProviderPriority = getStringArrayField(norm, "provider_priority")
	cfg.UseTLS = getBoolField(norm, "use_tls")
	cfg.UseSSL = getBoolField(norm, "use_ssl")
	cfg.SkipTLSVerify = getBoolField(norm, "skip_tls_verify")
	cfg.AdditionalData = norm.leftovers()
	if cfg.AdditionalData == nil {
		cfg.AdditionalData = map[string]any{}
	}
	cfg.ScheduleMode = strings.ToLower(getStringField(norm, "schedule_mode"))
	if cfg.ScheduleMode == "" {
		cfg.ScheduleMode = "repeat"
	}
	// Support nested wrapper keys often used by payloads such as "additional_data": {...} or "data": {...}
	// Merge their contents up to the top-level AdditionalData map so placeholders like {{data.key}} and {{key}} work.
	if inner, ok := cfg.AdditionalData["additional_data"]; ok {
		if m, ok := inner.(map[string]any); ok {
			for k, v := range m {
				cfg.AdditionalData[k] = v
			}
			delete(cfg.AdditionalData, "additional_data")
		}
	}
	if inner, ok := cfg.AdditionalData["data"]; ok {
		if m, ok := inner.(map[string]any); ok {
			for k, v := range m {
				cfg.AdditionalData[k] = v
			}
			delete(cfg.AdditionalData, "data")
		}
	}

	if err := applyPlaceholders(cfg, placeholderModeInitial); err != nil {
		return nil, err
	}

	if err := finalizeConfig(cfg); err != nil {
		return nil, err
	}

	if err := loadTemplateBodies(cfg); err != nil {
		return nil, err
	}
	cfg.captureRawContent()

	if err := applyPlaceholders(cfg, placeholderModePostFinalize); err != nil {
		return nil, err
	}
	resolveBodies(cfg)
	cfg.restoreRawContent()

	return cfg, nil
}

func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func mergeConfigMaps(base, override map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range override {
		if existing, ok := base[key]; ok {
			existingMap, okExisting := asMap(existing)
			valueMap, okValue := asMap(value)
			if okExisting && okValue {
				base[key] = mergeConfigMaps(existingMap, valueMap)
				continue
			}
		}
		base[key] = value
	}
	return base
}

func asMap(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case map[string]string:
		result := make(map[string]any, len(v))
		for key, val := range v {
			result[key] = val
		}
		return result, true
	default:
		return nil, false
	}
}

func finalizeConfig(cfg *EmailConfig) error {
	cfg.Provider = strings.ToLower(cfg.Provider)
	if cfg.Provider == "" {
		cfg.Provider = inferProvider(cfg.From, cfg.Username)
	}
	if cfg.Tags == nil {
		cfg.Tags = map[string]string{}
	}
	if cfg.HTTPAuthPrefix == "" {
		cfg.HTTPAuthPrefix = "Bearer"
	}
	applyProviderDefaults(cfg)
	applyHTTPProfile(cfg)

	if cfg.Transport == "" {
		if cfg.Endpoint != "" && looksLikeURL(cfg.Endpoint) {
			cfg.Transport = "http"
		} else if looksLikeURL(cfg.Host) {
			cfg.Transport = "http"
		} else {
			cfg.Transport = "smtp"
		}
	}

	if cfg.Transport != "http" {
		cfg.Transport = "smtp"
	}

	if cfg.Transport == "http" && cfg.Endpoint == "" {
		cfg.Endpoint = cfg.Host
	}

	if cfg.Transport == "http" && cfg.Endpoint != "" && !looksLikeURL(cfg.Endpoint) {
		cfg.Endpoint = "https://" + strings.TrimLeft(cfg.Endpoint, ":/")
	}

	if cfg.From == "" && cfg.Username != "" {
		cfg.From = cfg.Username
	}
	name, addr := splitAddress(cfg.From)
	if cfg.FromName == "" {
		cfg.FromName = name
	}
	if addr == "" {
		return errors.New("sender address is required")
	}
	cfg.From = addr
	if cfg.EnvelopeFrom == "" {
		cfg.EnvelopeFrom = addr
	}
	if cfg.ReturnPath != "" {
		cfg.EnvelopeFrom = cfg.ReturnPath
	}
	if cfg.Username == "" {
		cfg.Username = addr
	}
	if cfg.AWSRegion == "" {
		cfg.AWSRegion = inferAWSRegion(cfg.Endpoint)
	}

	if cfg.Subject == "" {
		cfg.Subject = "(no subject)"
	}
	resolveBodies(cfg)

	if len(cfg.To) == 0 {
		return errors.New("at least one recipient (to) is required")
	}

	if cfg.Transport == "smtp" {
		if cfg.Host == "" {
			return errors.New("smtp host is required")
		}
		if cfg.Port == 0 {
			if cfg.UseSSL {
				cfg.Port = 465
			} else if cfg.UseTLS {
				cfg.Port = 587
			} else {
				cfg.Port = 25
			}
		}
	} else {
		if cfg.Endpoint == "" {
			return errors.New("http endpoint is required when type=http")
		}
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.RetryCount <= 0 {
		cfg.RetryCount = 1
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 2 * time.Second
	}
	applyHTTPScalingDefaults(cfg)

	return nil
}

func applyHTTPScalingDefaults(cfg *EmailConfig) {
	if cfg.Transport != "http" {
		return
	}
	if cfg.MaxConnsPerHost == 0 {
		cfg.MaxConnsPerHost = 32
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 120
	}
	if cfg.MaxIdleConnsHost == 0 {
		cfg.MaxIdleConnsHost = 32
	}
}

func applyHTTPProfile(cfg *EmailConfig) {
	profile, ok := httpProviderProfiles[cfg.Provider]
	if !ok {
		return
	}
	if cfg.Transport == "" {
		cfg.Transport = "http"
	}
	if cfg.Transport != "http" {
		return
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = profile.Endpoint
	}
	if cfg.HTTPMethod == "" && profile.Method != "" {
		cfg.HTTPMethod = profile.Method
	}
	if cfg.PayloadFormat == "" && profile.PayloadFormat != "" {
		cfg.PayloadFormat = profile.PayloadFormat
	}
	if cfg.HTTPContentType == "" {
		cfg.HTTPContentType = profile.ContentType
	}
	if cfg.MaxConnsPerHost == 0 && profile.Endpoint != "" {
		cfg.MaxConnsPerHost = 32
	}
	if cfg.MaxIdleConns == 0 && profile.Endpoint != "" {
		cfg.MaxIdleConns = 120
	}
	if cfg.MaxIdleConnsHost == 0 && profile.Endpoint != "" {
		cfg.MaxIdleConnsHost = 32
	}
	if cfg.Provider == "ses" || cfg.Provider == "aws_ses" || cfg.Provider == "amazon_ses" {
		if cfg.HTTPAuth == "" {
			cfg.HTTPAuth = "aws_sigv4"
		}
		if cfg.AWSRegion == "" {
			cfg.AWSRegion = inferAWSRegion(cfg.Endpoint)
		}
	}
	if cfg.Provider == "postmark" && cfg.HTTPAuth == "" {
		cfg.HTTPAuth = "api_key_header"
		cfg.HTTPAuthHeader = "X-Postmark-Server-Token"
	}
	if cfg.Provider == "resend" && cfg.HTTPAuth == "" {
		cfg.HTTPAuth = "bearer"
	}
	if cfg.Provider == "sparkpost" && cfg.HTTPAuth == "" {
		cfg.HTTPAuth = "bearer"
	}
	// Seed sensible per-provider scaling defaults if not provided.
	switch cfg.Provider {
	case "ses", "aws_ses", "amazon_ses", "sendgrid", "sparkpost", "postmark", "resend", "mailgun":
		if cfg.MaxConnsPerHost == 0 {
			cfg.MaxConnsPerHost = 64
		}
		if cfg.MaxIdleConns == 0 {
			cfg.MaxIdleConns = 200
		}
		if cfg.MaxIdleConnsHost == 0 {
			cfg.MaxIdleConnsHost = 64
		}
	case "brevo", "sendinblue", "mailtrap":
		if cfg.MaxConnsPerHost == 0 {
			cfg.MaxConnsPerHost = 32
		}
		if cfg.MaxIdleConns == 0 {
			cfg.MaxIdleConns = 120
		}
		if cfg.MaxIdleConnsHost == 0 {
			cfg.MaxIdleConnsHost = 32
		}
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	for k, v := range profile.Headers {
		if _, exists := cfg.Headers[k]; !exists {
			cfg.Headers[k] = v
		}
	}
}

func applyProviderDefaults(cfg *EmailConfig) {
	if cfg.Provider == "" {
		return
	}
	if defaults, ok := providerDefaults[cfg.Provider]; ok {
		if cfg.Host == "" {
			cfg.Host = defaults.Host
		}
		if cfg.Port == 0 {
			cfg.Port = defaults.Port
		}
		if !cfg.UseTLS && !cfg.UseSSL {
			cfg.UseTLS = defaults.UseTLS
			cfg.UseSSL = defaults.UseSSL
		}
		if cfg.Transport == "" && defaults.Transport != "" {
			cfg.Transport = defaults.Transport
		}
		if cfg.Endpoint == "" && defaults.Endpoint != "" {
			cfg.Endpoint = defaults.Endpoint
		}
	}
}

func inferProvider(addresses ...string) string {
	for _, addr := range addresses {
		_, email := splitAddress(addr)
		if email == "" {
			continue
		}
		parts := strings.Split(email, "@")
		if len(parts) != 2 {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(parts[1]))
		if provider, ok := emailDomainMap[domain]; ok {
			return provider
		}
	}
	return ""
}

func resolveBodies(cfg *EmailConfig) {
	text := strings.TrimSpace(cfg.TextBody)
	html := strings.TrimSpace(cfg.HTMLBody)
	base := strings.TrimSpace(cfg.Body)

	if html == "" && looksLikeHTML(base) {
		html = base
	}
	if text == "" {
		if html == "" {
			text = base
		} else if base != "" && !looksLikeHTML(base) {
			text = base
		}
	}
	if text == "" && html == "" {
		text = "(empty message)"
	}

	cfg.TextBody = text
	cfg.HTMLBody = html
}

func loadTemplateBodies(cfg *EmailConfig) error {
	if path := strings.TrimSpace(cfg.HTMLTemplatePath); path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read html template %s: %w", path, err)
		}
		cfg.HTMLBody = string(content)
		log.Printf("Loaded HTML template: %s", path)
	}
	if path := strings.TrimSpace(cfg.TextTemplatePath); path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read text template %s: %w", path, err)
		}
		cfg.TextBody = string(content)
		log.Printf("Loaded text template: %s", path)
	}
	if path := strings.TrimSpace(cfg.BodyTemplatePath); path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read body template %s: %w", path, err)
		}
		cfg.Body = string(content)
		log.Printf("Loaded message template: %s", path)
	}
	return nil
}

type SendContext struct {
	JobID              string
	Step               string
	StepIndex          int
	PrevJobID          string
	RequireLastSuccess bool
	SkipAhead          bool
}

var errDeduplicated = errors.New("duplicate email skipped")

func prepareSendConfig(cfg *EmailConfig) (*EmailConfig, error) {
	cfgCopy := *cfg
	cfgCopy.AdditionalData = cloneAdditionalData(cfg.AdditionalData)
	cfgCopy.restoreRawContent()
	if err := applyPlaceholders(&cfgCopy, placeholderModePostFinalize); err != nil {
		return nil, err
	}
	resolveBodies(&cfgCopy)
	return &cfgCopy, nil
}

func dedupKeyFromConfig(cfg *EmailConfig, ctx *SendContext) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.ScheduleMode))
	if mode == "" || mode == "repeat" {
		return ""
	}
	step := ""
	if ctx != nil && strings.TrimSpace(ctx.Step) != "" {
		step = strings.TrimSpace(ctx.Step)
	} else if s, ok := cfg.AdditionalData["step"].(string); ok {
		step = strings.TrimSpace(s)
	}
	recipients := strings.ToLower(strings.Join(cfg.To, ","))
	subjectHash := sha256Hex([]byte(strings.ToLower(strings.TrimSpace(cfg.Subject))))
	bodyHash := sha256Hex([]byte(strings.ToLower(strings.TrimSpace(cfg.Body + cfg.TextBody + cfg.HTMLBody))))
	return fmt.Sprintf("%s|%s|%s|%s", recipients, strings.ToLower(step), subjectHash, bodyHash)
}

func sendEmail(cfg *EmailConfig, ctx *SendContext) error {
	preparedCfg, err := prepareSendConfig(cfg)
	if err != nil {
		return err
	}
	dedupKey := dedupKeyFromConfig(preparedCfg, ctx)
	if dedupKey != "" && dedupKeyExists(dedupKey) {
		if ctx != nil {
			log.Printf("sendEmail: duplicate detected job=%s step=%s, skipping", ctx.JobID, ctx.Step)
		} else {
			log.Printf("sendEmail: duplicate detected, skipping immediate send")
		}
		return errDeduplicated
	}
	// Build the ordered provider list to try.
	providers := make([]string, 0, len(preparedCfg.ProviderPriority)+1)
	if len(preparedCfg.ProviderPriority) > 0 {
		for _, p := range preparedCfg.ProviderPriority {
			if strings.TrimSpace(p) != "" {
				providers = append(providers, strings.ToLower(strings.TrimSpace(p)))
			}
		}
	}
	// If no explicit priority list, fall back to single provider from config (may be empty)
	if len(providers) == 0 {
		if preparedCfg.Provider != "" {
			providers = append(providers, preparedCfg.Provider)
		} else {
			// ensure we still try using the configured provider or host
			providers = append(providers, preparedCfg.Provider)
		}
	}

	var lastErr error
	for _, prov := range providers {
		// Try each provider in order; create a shallow copy to avoid mutating original cfg.
		cfgCopy := *preparedCfg
		cfgCopy.Provider = prov
		applyProviderDefaults(&cfgCopy)
		applyHTTPProfile(&cfgCopy)
		if err := finalizeConfig(&cfgCopy); err != nil {
			lastErr = err
			log.Printf("skipping provider %s due to config error: %v", prov, err)
			continue
		}

		for attempt := 1; attempt <= cfgCopy.RetryCount; attempt++ {
			var err error
			if cfgCopy.Transport == "http" {
				err = sendViaHTTP(&cfgCopy)
			} else {
				err = sendViaSMTP(&cfgCopy)
			}
			recordSendAttempt(ctx, &cfgCopy, attempt, err)
			if err == nil {
				if dedupKey != "" {
					markDedupKey(dedupKey)
				}
				return nil
			}
			lastErr = err
			if attempt < cfgCopy.RetryCount {
				delay := jitterBackoff(attempt, cfgCopy.RetryDelay, cfgCopy.MaxRetryDelay)
				log.Printf("provider=%s attempt %d/%d failed: %v (retrying in %s)", prov, attempt, cfgCopy.RetryCount, err, delay)
				time.Sleep(delay)
			}
		}
		log.Printf("provider %s exhausted, trying next provider if any", prov)
	}
	return lastErr
}

// jitterBackoff uses full jitter strategy: random[0, min(maxDelay, base*2^(attempt-1))].
func jitterBackoff(attempt int, base time.Duration, maxDelay time.Duration) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	factor := 1 << (attempt - 1)
	upper := time.Duration(factor) * base
	if maxDelay > 0 && upper > maxDelay {
		upper = maxDelay
	}
	if upper <= 0 {
		return 0
	}
	j := time.Duration(mrand.Int63n(int64(upper) + 1))
	return j
}

func sendViaSMTP(cfg *EmailConfig) error {
	msg, err := buildMessage(cfg)
	if err != nil {
		return err
	}
	recipients, err := gatherRecipients(cfg)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return errors.New("no valid recipients found")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var client *smtp.Client
	if cfg.UseSSL {
		client, err = dialTLSClient(cfg, addr)
	} else {
		client, err = dialPlainClient(cfg, addr)
	}
	if err != nil {
		return err
	}
	defer client.Quit()

	if cfg.UseTLS && !cfg.UseSSL {
		tlsConfig := &tls.Config{ServerName: cfg.Host, InsecureSkipVerify: cfg.SkipTLSVerify}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}

	if cfg.Username != "" && cfg.Password != "" {
		auth, err := buildSMTPAuth(cfg)
		if err != nil {
			return err
		}
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}

	if err := client.Mail(cfg.EnvelopeFrom); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	return nil
}

func sendViaHTTP(cfg *EmailConfig) error {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return errors.New("http endpoint is required")
	}
	if len(cfg.QueryParams) > 0 {
		if parsed, err := url.Parse(endpoint); err == nil {
			query := parsed.Query()
			for k, v := range cfg.QueryParams {
				query.Set(k, v)
			}
			parsed.RawQuery = query.Encode()
			endpoint = parsed.String()
		}
	}

	payload, hintedType, err := cfg.resolveHTTPPayload()
	if err != nil {
		return err
	}
	bodyBytes, finalType, err := encodePayload(payload, hintedType)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(cfg.HTTPMethod, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	if len(cfg.Headers) == 0 {
		cfg.Headers = map[string]string{}
	}
	contentTypeSet := false
	if finalType != "" {
		req.Header.Set("Content-Type", finalType)
		contentTypeSet = true
	}
	for k, v := range cfg.Headers {
		if strings.EqualFold(k, "Content-Type") {
			contentTypeSet = true
		}
		req.Header.Set(k, v)
	}
	if !contentTypeSet {
		req.Header.Set("Content-Type", "application/json")
	}
	applyAuthHeaders(req, cfg, bodyBytes)

	client := getHTTPClient(cfg)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		reqID := resp.Header.Get("x-amzn-requestid")
		if reqID == "" {
			reqID = resp.Header.Get("x-request-id")
		}
		if reqID != "" {
			return fmt.Errorf("http send failed: %s request_id=%s body=%s", resp.Status, reqID, strings.TrimSpace(string(respBody)))
		}
		return fmt.Errorf("http send failed: %s body=%s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if id := resp.Header.Get("x-amzn-requestid"); id != "" {
		log.Printf("http send ok (request_id=%s)", id)
	}
	return nil
}

func getHTTPClient(cfg *EmailConfig) *http.Client {
	key := httpClientKey(cfg)
	httpClientMu.Lock()
	if client, ok := httpClientCache[key]; ok {
		httpClientMu.Unlock()
		return client
	}
	transport := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: cfg.SkipTLSVerify},
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        choosePositive(cfg.MaxIdleConns, 200),
		MaxIdleConnsPerHost: choosePositive(cfg.MaxIdleConnsHost, 32),
		MaxConnsPerHost:     cfg.MaxConnsPerHost,
		DisableKeepAlives:   cfg.DisableKeepAlives,
	}
	client := &http.Client{Timeout: cfg.Timeout, Transport: transport}
	httpClientCache[key] = client
	httpClientMu.Unlock()
	return client
}

func httpClientKey(cfg *EmailConfig) string {
	host := cfg.Host
	if cfg.Endpoint != "" {
		if parsed, err := url.Parse(cfg.Endpoint); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
	}
	return fmt.Sprintf("host-%s-tls-%t-maxc-%d-idle-%d-idlehost-%d-noka-%t-timeout-%d", host, cfg.SkipTLSVerify, cfg.MaxConnsPerHost, cfg.MaxIdleConns, cfg.MaxIdleConnsHost, cfg.DisableKeepAlives, cfg.Timeout)
}

func choosePositive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (cfg *EmailConfig) resolveHTTPPayload() (any, string, error) {
	if cfg.HTTPPayload != nil {
		return cfg.HTTPPayload, pickContentType(cfg.HTTPContentType, ""), nil
	}
	if cfg.PayloadFormat != "" {
		if builder, ok := httpPayloadBuilders[cfg.PayloadFormat]; ok {
			payload, contentType, err := builder(cfg)
			return payload, pickContentType(cfg.HTTPContentType, contentType), err
		}
	}
	if builder, ok := httpPayloadBuilders[cfg.Provider]; ok {
		payload, contentType, err := builder(cfg)
		return payload, pickContentType(cfg.HTTPContentType, contentType), err
	}
	payload, err := buildHTTPPayload(cfg)
	return payload, pickContentType(cfg.HTTPContentType, ""), err
}

func encodePayload(payload any, contentType string) ([]byte, string, error) {
	switch v := payload.(type) {
	case nil:
		return []byte{}, contentType, nil
	case []byte:
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return v, contentType, nil
	case string:
		if contentType == "" {
			contentType = "text/plain"
		}
		return []byte(v), contentType, nil
	case url.Values:
		if contentType == "" {
			contentType = "application/x-www-form-urlencoded"
		}
		return []byte(v.Encode()), contentType, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, "", err
		}
		if contentType == "" {
			contentType = "application/json"
		}
		return data, contentType, nil
	}
}

func pickContentType(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

// ---------- normalization helpers ----------

func sanitizeKey(key string) string {
	lower := strings.ToLower(key)
	var b strings.Builder
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func getStringField(norm *normalizedConfig, canonical string) string {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

func getStringArrayField(norm *normalizedConfig, canonical string) []string {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return nil
	}
	return normalizeStringSlice(val)
}

func normalizeStringSlice(val any) []string {
	switch v := val.(type) {
	case string:
		return splitList(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(entry); trimmed != "" {
					out = append(out, trimmed)
				}
			default:
				out = append(out, strings.TrimSpace(fmt.Sprint(entry)))
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return []string{strings.TrimSpace(fmt.Sprint(v))}
	}
}

type simpleAddress struct {
	Name  string
	Email string
}

func parseAddressList(values []string) []simpleAddress {
	var result []simpleAddress
	for _, raw := range values {
		name, addr := splitAddress(raw)
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		result = append(result, simpleAddress{Name: strings.TrimSpace(name), Email: addr})
	}
	return result
}

func firstAddressEntry(values []string) simpleAddress {
	list := parseAddressList(values)
	if len(list) == 0 {
		return simpleAddress{}
	}
	return list[0]
}

func addressMaps(addresses []simpleAddress, emailKey, nameKey string) []map[string]string {
	result := make([]map[string]string, 0, len(addresses))
	for _, addr := range addresses {
		entry := map[string]string{emailKey: addr.Email}
		if addr.Name != "" {
			entry[nameKey] = addr.Name
		}
		result = append(result, entry)
	}
	return result
}

func singleAddressMap(addr simpleAddress, emailKey, nameKey string) map[string]string {
	if addr.Email == "" {
		return nil
	}
	entry := map[string]string{emailKey: addr.Email}
	if addr.Name != "" {
		entry[nameKey] = addr.Name
	}
	return entry
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getIntField(norm *normalizedConfig, canonical string) int {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}

func getBoolField(norm *normalizedConfig, canonical string) bool {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return false
	}
	return normalizeBool(val)
}

func normalizeBool(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "yes" || lower == "1"
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return false
}

func getDurationField(norm *normalizedConfig, canonical string) time.Duration {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return time.Duration(v) * time.Second
	case int:
		return time.Duration(v) * time.Second
	case string:
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return time.Duration(i) * time.Second
		}
	}
	return 0
}

func getStringMapField(norm *normalizedConfig, canonical string) map[string]string {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return nil
	}
	result := map[string]string{}
	switch v := val.(type) {
	case map[string]any:
		for key, value := range v {
			result[key] = strings.TrimSpace(fmt.Sprint(value))
		}
	case map[string]string:
		for key, value := range v {
			result[key] = strings.TrimSpace(value)
		}
	case []any:
		for _, item := range v {
			switch entry := item.(type) {
			case string:
				k, val := splitKeyValue(entry)
				if k != "" {
					result[k] = val
				}
			case map[string]any:
				for key, value := range entry {
					result[key] = strings.TrimSpace(fmt.Sprint(value))
				}
			}
		}
	case string:
		pairs := strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' || r == '\n' })
		for _, pair := range pairs {
			k, val := splitKeyValue(pair)
			if k != "" {
				result[k] = val
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func getObjectField(norm *normalizedConfig, canonical string) map[string]any {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return nil
	}
	return normalizeObject(val)
}

func mergeAdditional(base map[string]any, extras map[string]any, overwrite bool) map[string]any {
	if len(extras) == 0 {
		return base
	}
	if base == nil {
		base = make(map[string]any, len(extras))
	}
	for k, v := range extras {
		if !overwrite {
			if _, exists := base[k]; exists {
				continue
			}
		}
		base[k] = v
	}
	return base
}

func (cfg *EmailConfig) captureRawContent() {
	cfg.RawSubject = cfg.Subject
	cfg.RawBody = cfg.Body
	cfg.RawTextBody = cfg.TextBody
	cfg.RawHTMLBody = cfg.HTMLBody
	if cfg.HTTPPayload != nil {
		if cloned, ok := cloneArbitraryValue(cfg.HTTPPayload).(map[string]any); ok {
			cfg.RawHTTPPayload = cloned
		} else {
			cfg.RawHTTPPayload = nil
		}
	} else {
		cfg.RawHTTPPayload = nil
	}
}

func (cfg *EmailConfig) restoreRawContent() {
	if cfg.RawSubject != "" {
		cfg.Subject = cfg.RawSubject
	}
	if cfg.RawBody != "" {
		cfg.Body = cfg.RawBody
	}
	if cfg.RawTextBody != "" {
		cfg.TextBody = cfg.RawTextBody
	}
	if cfg.RawHTMLBody != "" {
		cfg.HTMLBody = cfg.RawHTMLBody
	}
	if cfg.RawHTTPPayload != nil {
		if cloned, ok := cloneArbitraryValue(cfg.RawHTTPPayload).(map[string]any); ok {
			cfg.HTTPPayload = cloned
		} else {
			cfg.HTTPPayload = nil
		}
	}
}

func cloneAdditionalData(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	copy := make(map[string]any, len(src))
	for k, v := range src {
		copy[k] = cloneArbitraryValue(v)
	}
	return copy
}

func cloneArbitraryValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		return cloneAdditionalData(v)
	case map[string]string:
		dup := make(map[string]string, len(v))
		for k, s := range v {
			dup[k] = s
		}
		return dup
	case []any:
		dup := make([]any, len(v))
		for i, item := range v {
			dup[i] = cloneArbitraryValue(item)
		}
		return dup
	case []string:
		dup := make([]string, len(v))
		copy(dup, v)
		return dup
	default:
		return v
	}
}

func normalizeObject(val any) map[string]any {
	switch v := val.(type) {
	case map[string]any:
		return v
	case map[string]string:
		result := make(map[string]any, len(v))
		for k, value := range v {
			result[k] = value
		}
		return result
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
	}
	return nil
}

func signAWSv4(req *http.Request, body []byte, cfg *EmailConfig) error {
	region := strings.TrimSpace(cfg.AWSRegion)
	if region == "" {
		region = inferAWSRegion(req.URL.String())
	}
	if region == "" {
		return errors.New("aws region required for sigv4")
	}
	access := strings.TrimSpace(cfg.AWSAccessKey)
	secret := strings.TrimSpace(cfg.AWSSecretKey)
	if access == "" || secret == "" {
		return errors.New("aws credentials required for sigv4")
	}
	service := "ses"
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(body)
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if cfg.AWSSessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", cfg.AWSSessionToken)
	}

	canonicalHeaders, signedHeaders := canonicalizeHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQuery(req.URL.RawQuery),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := awsSigningKey(secret, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", access, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
	return nil
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	escaped := url.PathEscape(path)
	return strings.ReplaceAll(escaped, "%2F", "/")
}

func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		sortedVals := values[k]
		sort.Strings(sortedVals)
		for _, v := range sortedVals {
			parts = append(parts, escapeQuery(k)+"="+escapeQuery(v))
		}
	}
	return strings.Join(parts, "&")
}

func escapeQuery(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func canonicalizeHeaders(req *http.Request) (string, string) {
	var keys []string
	headers := map[string][]string{}
	for k, vals := range req.Header {
		lower := strings.ToLower(k)
		keys = append(keys, lower)
		headers[lower] = vals
	}
	if _, ok := headers["host"]; !ok {
		keys = append(keys, "host")
		headers["host"] = []string{req.URL.Host}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	var signed []string
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		signed = append(signed, k)
		values := headers[k]
		for i, v := range values {
			values[i] = strings.TrimSpace(v)
		}
		canonical.WriteString(k)
		canonical.WriteString(":")
		canonical.WriteString(strings.Join(values, ","))
		canonical.WriteString("\n")
	}
	return canonical.String(), strings.Join(signed, ";")
}

func awsSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func inferAWSRegion(endpoint string) string {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	if endpoint == "" {
		return ""
	}
	if strings.Contains(endpoint, "email-") && strings.Contains(endpoint, ".amazonaws.com") {
		re := regexp.MustCompile(`email-([a-z0-9-]+)\.amazonaws\.com`)
		if match := re.FindStringSubmatch(endpoint); len(match) == 2 {
			return match[1]
		}
	}
	re := regexp.MustCompile(`\.([a-z0-9-]+)\.amazonaws\.com`)
	if match := re.FindStringSubmatch(endpoint); len(match) == 2 {
		return match[1]
	}
	return ""
}

func registerSliceValue(values map[string]string, source []string, overwrite bool, keys ...string) {
	clean := make([]string, 0, len(source))
	for _, item := range source {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return
	}
	registerValue(values, strings.Join(clean, ","), overwrite, keys...)
}

func registerValue(values map[string]string, value string, overwrite bool, keys ...string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, key := range keys {
		normalized := normalizePlaceholderKey(key)
		if normalized == "" {
			continue
		}
		if !overwrite {
			if _, exists := values[normalized]; exists {
				continue
			}
		}
		values[normalized] = value
	}
}

func flattenAdditionalData(values map[string]string, data map[string]any) {
	var walker func(prefix string, input any)
	walker = func(prefix string, input any) {
		switch v := input.(type) {
		case map[string]any:
			for key, val := range v {
				next := normalizePlaceholderKey(key)
				if next == "" {
					continue
				}
				fullKey := next
				if prefix != "" {
					fullKey = prefix + "." + next
				}
				walker(fullKey, val)
			}
		case []any:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				parts = append(parts, strings.TrimSpace(fmt.Sprint(item)))
			}
			registerAdditionalValue(values, prefix, strings.Join(parts, ","))
		case []string:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				parts = append(parts, strings.TrimSpace(item))
			}
			registerAdditionalValue(values, prefix, strings.Join(parts, ","))
		default:
			registerAdditionalValue(values, prefix, strings.TrimSpace(fmt.Sprint(input)))
		}
	}
	walker("", data)
}

func registerAdditionalValue(values map[string]string, key, value string) {
	if key == "" {
		return
	}
	registerValue(values, value, false, key)
	registerValue(values, value, true, "data."+key)
}

func splitKeyValue(input string) (string, string) {
	if strings.Contains(input, "=") {
		parts := strings.SplitN(input, "=", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if strings.Contains(input, ":") {
		parts := strings.SplitN(input, ":", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", ""
}

func ensureStringMap(input map[string]string) map[string]string {
	if input != nil {
		return input
	}
	return map[string]string{}
}

func fallbackBody(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty message)"
	}
	return value
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := values[key]; ok {
			switch v := raw.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

// ---------- misc helpers ----------

func splitAddress(value string) (string, string) {
	if strings.TrimSpace(value) == "" {
		return "", ""
	}
	addr, err := mail.ParseAddress(value)
	if err != nil {
		return "", strings.TrimSpace(value)
	}
	return addr.Name, addr.Address
}

func looksLikeHTML(body string) bool {
	body = strings.TrimSpace(body)
	return strings.HasPrefix(body, "<") && strings.Contains(body, ">")
}

func looksLikeURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func randomBoundary(prefix string) string {
	buf := make([]byte, 12)
	if _, err := cryptorand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(buf))
}

func (cfg *EmailConfig) TransportDetails() string {
	if cfg.Transport == "http" {
		return cfg.Endpoint
	}
	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
}

func (cfg *EmailConfig) ProviderOrHost() string {
	if cfg.Provider != "" {
		return cfg.Provider
	}
	return cfg.Host
}
