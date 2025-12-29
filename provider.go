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

// ProviderSetting captures smart defaults for known providers.
type ProviderSetting struct {
	Host      string
	Port      int
	UseTLS    bool
	UseSSL    bool
	Transport string
	Endpoint  string
	// Capacity is an approximate number of sends the provider can handle in the selection window.
	Capacity int `json:"capacity"`
	// Cost is a relative cost metric; higher cost will penalize selection when cost-aware routing is used.
	Cost float64 `json:"cost"`
}

type payloadBuilder func(*EmailConfig) (any, string, error)

type httpProviderProfile struct {
	Endpoint      string
	Method        string
	ContentType   string
	PayloadFormat string
	Headers       map[string]string
}

var providerDefaults = map[string]ProviderSetting{
	// Traditional SMTP providers
	"gmail":     {Host: "smtp.gmail.com", Port: 587, UseTLS: true},
	"google":    {Host: "smtp.gmail.com", Port: 587, UseTLS: true},
	"outlook":   {Host: "smtp-mail.outlook.com", Port: 587, UseTLS: true},
	"office365": {Host: "smtp.office365.com", Port: 587, UseTLS: true},
	"yahoo":     {Host: "smtp.mail.yahoo.com", Port: 587, UseTLS: true},
	"zoho":      {Host: "smtp.zoho.com", Port: 587, UseTLS: true},
	"fastmail":  {Host: "smtp.fastmail.com", Port: 465, UseSSL: true},

	// Transactional email services - HTTP API preferred
	"sendgrid":   {Transport: "http", Endpoint: "https://api.sendgrid.com/v3/mail/send"},
	"mailgun":    {Transport: "http", Endpoint: "https://api.mailgun.net/v3"},
	"postmark":   {Transport: "http", Endpoint: "https://api.postmarkapp.com/email"},
	"sparkpost":  {Transport: "http", Endpoint: "https://api.sparkpost.com/api/v1/transmissions"},
	"resend":     {Transport: "http", Endpoint: "https://api.resend.com/emails"},
	"mailtrap":   {Transport: "http", Endpoint: "https://send.api.mailtrap.io/api/send"},
	"sendinblue": {Transport: "http", Endpoint: "https://api.sendinblue.com/v3/smtp/email"},
	"brevo":      {Transport: "http", Endpoint: "https://api.brevo.com/v3/smtp/email"},
	"mailjet":    {Transport: "http", Endpoint: "https://api.mailjet.com/v3.1/send"},

	// AWS SES - HTTP API preferred
	"amazon_ses": {Transport: "http", Endpoint: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails"},
	"amazon":     {Transport: "http", Endpoint: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails"},
	"aws_ses":    {Transport: "http", Endpoint: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails"},
	"ses":        {Transport: "http", Endpoint: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails"},

	// Other providers
	"elasticemail": {Transport: "http", Endpoint: "https://api.elasticemail.com/v2/email/send"},
	"protonmail":   {Transport: "http", Endpoint: "https://api.protonmail.ch"},
	"mailersend":   {Transport: "http", Endpoint: "https://api.mailersend.com/v1/email"},
	"pepipost":     {Transport: "http", Endpoint: "https://api.pepipost.com/v5/mail/send"},
	"sendpulse":    {Transport: "http", Endpoint: "https://api.sendpulse.com/smtp/emails"},
	"mandrill":     {Transport: "http", Endpoint: "https://mandrillapp.com/api/1.0/messages/send.json"},
	"socketlabs":   {Transport: "http", Endpoint: "https://inject.socketlabs.com/api/v1/email"},
	"smtp2go":      {Transport: "http", Endpoint: "https://api.smtp2go.com/v3/email/send"},
}

var httpProviderProfiles = map[string]httpProviderProfile{
	"sendgrid": {
		Endpoint:      "https://api.sendgrid.com/v3/mail/send",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sendgrid",
		Headers: map[string]string{
			"Authorization": "Bearer ${API_KEY}",
		},
	},
	"ses": {
		Endpoint:      "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sesv2",
	},
	"aws_ses": {
		Endpoint:      "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sesv2",
	},
	"amazon_ses": {
		Endpoint:      "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sesv2",
	},
	"brevo": {
		Endpoint:      "https://api.brevo.com/v3/smtp/email",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "brevo",
		Headers: map[string]string{
			"accept":  "application/json",
			"api-key": "${API_KEY}",
		},
	},
	"sendinblue": {
		Endpoint:      "https://api.sendinblue.com/v3/smtp/email",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "brevo",
		Headers: map[string]string{
			"accept":  "application/json",
			"api-key": "${API_KEY}",
		},
	},
	"mailtrap": {
		Endpoint:      "https://send.api.mailtrap.io/api/send",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "mailtrap",
		Headers: map[string]string{
			"Api-Token": "${API_KEY}",
		},
	},
	"postmark": {
		Endpoint:      "https://api.postmarkapp.com/email",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "postmark",
		Headers: map[string]string{
			"X-Postmark-Server-Token": "${API_KEY}",
		},
	},
	"sparkpost": {
		Endpoint:      "https://api.sparkpost.com/api/v1/transmissions",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sparkpost",
		Headers: map[string]string{
			"Authorization": "${API_KEY}",
		},
	},
	"resend": {
		Endpoint:      "https://api.resend.com/emails",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "resend",
		Headers: map[string]string{
			"Authorization": "Bearer ${API_KEY}",
		},
	},
	"mailgun": {
		Endpoint:      "https://api.mailgun.net/v3",
		Method:        http.MethodPost,
		ContentType:   "application/x-www-form-urlencoded",
		PayloadFormat: "mailgun",
		Headers: map[string]string{
			"Authorization": "Basic ${API_KEY}",
		},
	},
	"mailjet": {
		Endpoint:      "https://api.mailjet.com/v3.1/send",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "mailjet",
		Headers: map[string]string{
			"Authorization": "Basic ${API_KEY}",
		},
	},
	"elasticemail": {
		Endpoint:      "https://api.elasticemail.com/v2/email/send",
		Method:        http.MethodPost,
		ContentType:   "application/x-www-form-urlencoded",
		PayloadFormat: "elasticemail",
	},
	"mailersend": {
		Endpoint:      "https://api.mailersend.com/v1/email",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "mailersend",
		Headers: map[string]string{
			"Authorization": "Bearer ${API_KEY}",
		},
	},
	"mandrill": {
		Endpoint:      "https://mandrillapp.com/api/1.0/messages/send.json",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "mandrill",
	},
	"smtp2go": {
		Endpoint:      "https://api.smtp2go.com/v3/email/send",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "smtp2go",
		Headers: map[string]string{
			"X-Smtp2go-Api-Key": "${API_KEY}",
		},
	},
}

var httpPayloadBuilders = map[string]payloadBuilder{
	"sendgrid":     buildSendGridPayload,
	"brevo":        buildBrevoPayload,
	"sendinblue":   buildBrevoPayload,
	"mailtrap":     buildMailtrapPayload,
	"sesv2":        buildSESPayload,
	"ses":          buildSESPayload,
	"aws_ses":      buildSESPayload,
	"amazon_ses":   buildSESPayload,
	"postmark":     buildPostmarkPayload,
	"sparkpost":    buildSparkPostPayload,
	"resend":       buildResendPayload,
	"mailgun":      buildMailgunPayload,
	"mailjet":      buildMailjetPayload,
	"elasticemail": buildElasticEmailPayload,
	"mailersend":   buildMailerSendPayload,
	"mandrill":     buildMandrillPayload,
	"smtp2go":      buildSMTP2GOPayload,
}

var (
	httpClientMu    sync.Mutex
	httpClientCache = map[string]*http.Client{}
)

var emailDomainMap = map[string]string{
	"gmail.com":      "gmail",
	"googlemail.com": "gmail",
	"outlook.com":    "outlook",
	"hotmail.com":    "outlook",
	"live.com":       "outlook",
	"office365.com":  "office365",
	"yahoo.com":      "yahoo",
	"yandex.com":     "mailgun",
	"zoho.com":       "zoho",
	"pm.me":          "protonmail",
	"protonmail.com": "protonmail",
	"fastmail.com":   "fastmail",
	"hey.com":        "mailgun",
	"icloud.com":     "mailgun",
	"me.com":         "mailgun",
	"mac.com":        "mailgun",
	"gmx.com":        "mailgun",
	"aol.com":        "mailgun",
}

// RegisterProviderDefault adds or updates a provider's default settings.
// This allows extending the system with new email providers without modifying the core code.
func RegisterProviderDefault(provider string, setting ProviderSetting) {
	if provider == "" {
		return
	}
	providerDefaults[strings.ToLower(provider)] = setting
}

// RegisterHTTPProviderProfile adds or updates an HTTP provider profile.
// This enables support for new HTTP-based email services.
func RegisterHTTPProviderProfile(provider string, profile httpProviderProfile) {
	if provider == "" {
		return
	}
	httpProviderProfiles[strings.ToLower(provider)] = profile
}

// RegisterHTTPPayloadBuilder adds or updates a payload builder function for an HTTP provider.
// This allows custom payload formatting for new or existing providers.
func RegisterHTTPPayloadBuilder(provider string, builder payloadBuilder) {
	if provider == "" || builder == nil {
		return
	}
	httpPayloadBuilders[strings.ToLower(provider)] = builder
}

// RegisterEmailDomainMap adds or updates domain-to-provider mappings.
// This helps auto-detect providers based on email domains.
func RegisterEmailDomainMap(domain, provider string) {
	if domain == "" || provider == "" {
		return
	}
	emailDomainMap[strings.ToLower(domain)] = strings.ToLower(provider)
}

func buildHTTPPayload(cfg *EmailConfig) (map[string]any, error) {
	payload := map[string]any{
		"from":        cfg.From,
		"from_name":   cfg.FromName,
		"reply_to":    cfg.ReplyTo,
		"to":          cfg.To,
		"cc":          cfg.CC,
		"bcc":         cfg.BCC,
		"subject":     cfg.Subject,
		"text_body":   cfg.TextBody,
		"html_body":   cfg.HTMLBody,
		"provider":    cfg.Provider,
		"attachments": []map[string]string{},
	}

	if len(cfg.Attachments) > 0 {
		files := make([]map[string]string, 0, len(cfg.Attachments))
		for _, att := range cfg.Attachments {
			encoded, err := encodeAttachment(att)
			if err != nil {
				return nil, err
			}
			files = append(files, encoded)
		}
		payload["attachments"] = files
	}

	payload = mergeAdditional(payload, cfg.AdditionalData, false)

	return payload, nil
}

func buildSendGridPayload(cfg *EmailConfig) (any, string, error) {
	personalization := map[string]any{
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

	payload := map[string]any{
		"personalizations": []any{personalization},
		"from":             fromEntry,
		"content":          contents,
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["reply_to"] = singleAddressMap(reply, "email", "name")
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
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

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildMailtrapPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)
	sender := singleAddressMap(simpleAddress{Name: fromName, Email: fromEmail}, "email", "name")
	payload := map[string]any{
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
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		payload["text"] = fallbackBody(cfg.TextBody)
	}

	if len(cfg.CC) > 0 {
		payload["cc"] = addressMaps(parseAddressList(cfg.CC), "email", "name")
	}
	if len(cfg.BCC) > 0 {
		payload["bcc"] = addressMaps(parseAddressList(cfg.BCC), "email", "name")
	}
	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["reply_to"] = singleAddressMap(reply, "email", "name")
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]any, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]any{
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

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildBrevoPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)
	sender := singleAddressMap(simpleAddress{Name: fromName, Email: fromEmail}, "email", "name")
	payload := map[string]any{
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
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		payload["textContent"] = fallbackBody(cfg.TextBody)
	}

	if len(cfg.CC) > 0 {
		payload["cc"] = addressMaps(parseAddressList(cfg.CC), "email", "name")
	}
	if len(cfg.BCC) > 0 {
		payload["bcc"] = addressMaps(parseAddressList(cfg.BCC), "email", "name")
	}
	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["replyTo"] = singleAddressMap(reply, "email", "name")
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]any, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]any{
				"name":    att.Filename,
				"content": att.Content,
			}
			attachments = append(attachments, entry)
		}
		payload["attachment"] = attachments
	}

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildSESPayload(cfg *EmailConfig) (any, string, error) {
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
	payload := map[string]any{
		"Content": map[string]any{
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

func buildPostmarkPayload(cfg *EmailConfig) (any, string, error) {
	payload := map[string]any{
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
	if len(cfg.Headers) > 0 {
		var headers []map[string]string
		for k, v := range cfg.Headers {
			headers = append(headers, map[string]string{"Name": k, "Value": v})
		}
		payload["Headers"] = headers
	}
	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
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
	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildSparkPostPayload(cfg *EmailConfig) (any, string, error) {
	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	inlineImages := []map[string]string{}
	attachments := []map[string]string{}
	for _, att := range encoded {
		entry := map[string]string{
			"type": att.MIMEType,
			"name": att.Filename,
			"data": att.Content,
		}
		if att.Inline {
			if att.ContentID != "" {
				entry["name"] = att.ContentID
			}
			inlineImages = append(inlineImages, entry)
		} else {
			attachments = append(attachments, entry)
		}
	}

	fromName, fromEmail := splitAddress(cfg.From)
	content := map[string]any{
		"from":    map[string]string{"email": fromEmail, "name": fromName},
		"subject": cfg.Subject,
	}

	if cfg.HTMLBody != "" {
		content["html"] = cfg.HTMLBody
	}
	if cfg.TextBody != "" {
		content["text"] = cfg.TextBody
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		content["text"] = fallbackBody(cfg.TextBody)
	}

	if len(attachments) > 0 {
		content["attachments"] = attachments
	}
	if len(inlineImages) > 0 {
		content["inline_images"] = inlineImages
	}

	recipients := make([]map[string]any, 0, len(cfg.To))
	for _, addr := range cfg.To {
		recipients = append(recipients, map[string]any{
			"address": map[string]string{"email": strings.TrimSpace(addr)},
		})
	}

	payload := map[string]any{
		"recipients": recipients,
		"content":    content,
	}

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildResendPayload(cfg *EmailConfig) (any, string, error) {
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

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]any, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]any{
				"filename": att.Filename,
				"content":  att.Content,
			}
			attachments = append(attachments, entry)
		}
		payload["attachments"] = attachments
	}

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildMailgunPayload(cfg *EmailConfig) (any, string, error) {
	domain := strings.TrimSpace(firstString(cfg.AdditionalData, "domain", "mailgun_domain"))
	if domain == "" {
		domain = inferMailgunDomain(cfg.Endpoint)
	}
	if domain == "" {
		parts := strings.Split(cfg.From, "@")
		if len(parts) == 2 {
			domain = parts[1]
		}
	}
	if domain == "" {
		return nil, "", errors.New("mailgun domain is required (set 'domain' in payload or use from address)")
	}

	if cfg.Endpoint != "" && !strings.Contains(cfg.Endpoint, "/messages") {
		cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/") + "/" + domain + "/messages"
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

func buildMailjetPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)

	toList := make([]map[string]string, 0, len(cfg.To))
	for _, addr := range parseAddressList(cfg.To) {
		toList = append(toList, map[string]string{"Email": addr.Email, "Name": addr.Name})
	}

	message := map[string]any{
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
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		message["TextPart"] = fallbackBody(cfg.TextBody)
	}

	if len(cfg.CC) > 0 {
		ccList := make([]map[string]string, 0, len(cfg.CC))
		for _, addr := range parseAddressList(cfg.CC) {
			ccList = append(ccList, map[string]string{"Email": addr.Email, "Name": addr.Name})
		}
		message["Cc"] = ccList
	}

	if len(cfg.BCC) > 0 {
		bccList := make([]map[string]string, 0, len(cfg.BCC))
		for _, addr := range parseAddressList(cfg.BCC) {
			bccList = append(bccList, map[string]string{"Email": addr.Email, "Name": addr.Name})
		}
		message["Bcc"] = bccList
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		message["ReplyTo"] = map[string]string{"Email": reply.Email, "Name": reply.Name}
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]string, 0, len(encoded))
		inlineAttachments := make([]map[string]string, 0)

		for _, att := range encoded {
			entry := map[string]string{
				"ContentType":   att.MIMEType,
				"Filename":      att.Filename,
				"Base64Content": att.Content,
			}
			if att.Inline && att.ContentID != "" {
				entry["ContentID"] = att.ContentID
				inlineAttachments = append(inlineAttachments, entry)
			} else {
				attachments = append(attachments, entry)
			}
		}

		if len(attachments) > 0 {
			message["Attachments"] = attachments
		}
		if len(inlineAttachments) > 0 {
			message["InlinedAttachments"] = inlineAttachments
		}
	}

	payload := map[string]any{
		"Messages": []any{message},
	}

	return payload, "application/json", nil
}

func buildElasticEmailPayload(cfg *EmailConfig) (any, string, error) {
	form := url.Values{}
	form.Set("from", cfg.From)
	form.Set("fromName", cfg.FromName)
	form.Set("subject", cfg.Subject)

	for _, to := range cfg.To {
		form.Add("to", to)
	}

	if cfg.TextBody != "" {
		form.Set("bodyText", cfg.TextBody)
	}
	if cfg.HTMLBody != "" {
		form.Set("bodyHtml", cfg.HTMLBody)
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		form.Set("bodyText", fallbackBody(cfg.TextBody))
	}

	if len(cfg.CC) > 0 {
		form.Set("msgCC", strings.Join(cfg.CC, ","))
	}
	if len(cfg.BCC) > 0 {
		form.Set("msgBcc", strings.Join(cfg.BCC, ","))
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		form.Set("replyTo", reply.Email)
	}

	return form, "application/x-www-form-urlencoded", nil
}

func buildMailerSendPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)

	toList := make([]map[string]string, 0, len(cfg.To))
	for _, addr := range parseAddressList(cfg.To) {
		toList = append(toList, map[string]string{"email": addr.Email, "name": addr.Name})
	}

	payload := map[string]any{
		"from": map[string]string{
			"email": fromEmail,
			"name":  fromName,
		},
		"to":      toList,
		"subject": cfg.Subject,
	}

	if cfg.TextBody != "" {
		payload["text"] = cfg.TextBody
	}
	if cfg.HTMLBody != "" {
		payload["html"] = cfg.HTMLBody
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		payload["text"] = fallbackBody(cfg.TextBody)
	}

	if len(cfg.CC) > 0 {
		ccList := make([]map[string]string, 0, len(cfg.CC))
		for _, addr := range parseAddressList(cfg.CC) {
			ccList = append(ccList, map[string]string{"email": addr.Email, "name": addr.Name})
		}
		payload["cc"] = ccList
	}

	if len(cfg.BCC) > 0 {
		bccList := make([]map[string]string, 0, len(cfg.BCC))
		for _, addr := range parseAddressList(cfg.BCC) {
			bccList = append(bccList, map[string]string{"email": addr.Email, "name": addr.Name})
		}
		payload["bcc"] = bccList
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["reply_to"] = map[string]string{"email": reply.Email, "name": reply.Name}
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]any, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]any{
				"filename": att.Filename,
				"content":  att.Content,
			}
			if att.Inline && att.ContentID != "" {
				entry["id"] = att.ContentID
				entry["disposition"] = "inline"
			}
			attachments = append(attachments, entry)
		}
		payload["attachments"] = attachments
	}

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
}

func buildMandrillPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)

	toList := make([]map[string]string, 0, len(cfg.To))
	for _, addr := range parseAddressList(cfg.To) {
		toList = append(toList, map[string]string{
			"email": addr.Email,
			"name":  addr.Name,
			"type":  "to",
		})
	}

	for _, addr := range parseAddressList(cfg.CC) {
		toList = append(toList, map[string]string{
			"email": addr.Email,
			"name":  addr.Name,
			"type":  "cc",
		})
	}

	for _, addr := range parseAddressList(cfg.BCC) {
		toList = append(toList, map[string]string{
			"email": addr.Email,
			"name":  addr.Name,
			"type":  "bcc",
		})
	}

	message := map[string]any{
		"from_email": fromEmail,
		"from_name":  fromName,
		"to":         toList,
		"subject":    cfg.Subject,
	}

	if cfg.TextBody != "" {
		message["text"] = cfg.TextBody
	}
	if cfg.HTMLBody != "" {
		message["html"] = cfg.HTMLBody
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		message["text"] = fallbackBody(cfg.TextBody)
	}

	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		message["headers"] = map[string]string{"Reply-To": reply.Email}
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]string, 0, len(encoded))
		images := make([]map[string]string, 0)

		for _, att := range encoded {
			entry := map[string]string{
				"type":    att.MIMEType,
				"name":    att.Filename,
				"content": att.Content,
			}
			if att.Inline {
				images = append(images, entry)
			} else {
				attachments = append(attachments, entry)
			}
		}

		if len(attachments) > 0 {
			message["attachments"] = attachments
		}
		if len(images) > 0 {
			message["images"] = images
		}
	}

	apiKey := strings.TrimSpace(firstString(cfg.AdditionalData, "api_key", "mandrill_key"))
	payload := map[string]any{
		"key":     apiKey,
		"message": message,
	}

	return payload, "application/json", nil
}

func buildSMTP2GOPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)

	toList := make([]string, 0, len(cfg.To))
	for _, addr := range cfg.To {
		toList = append(toList, addr)
	}

	payload := map[string]any{
		"sender":  fromEmail,
		"to":      toList,
		"subject": cfg.Subject,
	}

	if fromName != "" {
		payload["sender_name"] = fromName
	}

	if cfg.TextBody != "" {
		payload["text_body"] = cfg.TextBody
	}
	if cfg.HTMLBody != "" {
		payload["html_body"] = cfg.HTMLBody
	}
	if cfg.TextBody == "" && cfg.HTMLBody == "" {
		payload["text_body"] = fallbackBody(cfg.TextBody)
	}

	if len(cfg.CC) > 0 {
		payload["cc"] = cfg.CC
	}
	if len(cfg.BCC) > 0 {
		payload["bcc"] = cfg.BCC
	}

	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]string, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]string{
				"filename": att.Filename,
				"fileblob": att.Content,
				"mimetype": att.MIMEType,
			}
			attachments = append(attachments, entry)
		}
		payload["attachments"] = attachments
	}

	payload = mergeAdditional(payload, cfg.AdditionalData, true)
	return payload, "application/json", nil
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
