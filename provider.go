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
	"gmail":        {Host: "smtp.gmail.com", Port: 587, UseTLS: true},
	"google":       {Host: "smtp.gmail.com", Port: 587, UseTLS: true},
	"outlook":      {Host: "smtp-mail.outlook.com", Port: 587, UseTLS: true},
	"office365":    {Host: "smtp.office365.com", Port: 587, UseTLS: true},
	"yahoo":        {Host: "smtp.mail.yahoo.com", Port: 587, UseTLS: true},
	"zoho":         {Host: "smtp.zoho.com", Port: 587, UseTLS: true},
	"mailtrap":     {Host: "smtp.mailtrap.io", Port: 2525, UseTLS: true},
	"sendgrid":     {Host: "smtp.sendgrid.net", Port: 587, UseTLS: true},
	"mailgun":      {Host: "smtp.mailgun.org", Port: 587, UseTLS: true},
	"postmark":     {Host: "smtp.postmarkapp.com", Port: 587, UseTLS: true},
	"sparkpost":    {Host: "smtp.sparkpostmail.com", Port: 587, UseTLS: true},
	"amazon_ses":   {Host: "email-smtp.us-east-1.amazonaws.com", Port: 587, UseTLS: true},
	"amazon":       {Host: "email-smtp.us-east-1.amazonaws.com", Port: 587, UseTLS: true},
	"aws_ses":      {Transport: "http", Endpoint: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails"},
	"ses":          {Transport: "http", Endpoint: "https://email.us-east-1.amazonaws.com/v2/email/outbound-emails"},
	"fastmail":     {Host: "smtp.fastmail.com", Port: 465, UseSSL: true},
	"protonmail":   {Transport: "http", Endpoint: "https://api.protonmail.ch"},
	"sendinblue":   {Host: "smtp-relay.sendinblue.com", Port: 587, UseTLS: true},
	"brevo":        {Host: "smtp-relay.brevo.com", Port: 587, UseTLS: true},
	"mailjet":      {Host: "in-v3.mailjet.com", Port: 587, UseTLS: true},
	"elasticemail": {Host: "smtp.elasticemail.com", Port: 2525, UseTLS: true},
}

var httpProviderProfiles = map[string]httpProviderProfile{
	"sendgrid": {
		Endpoint:      "https://api.sendgrid.com/v3/mail/send",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sendgrid",
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
			"accept": "application/json",
		},
	},
	"sendinblue": {
		Endpoint:      "https://api.sendinblue.com/v3/smtp/email",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "brevo",
		Headers: map[string]string{
			"accept": "application/json",
		},
	},
	"mailtrap": {
		Endpoint:      "https://send.api.mailtrap.io/api/send",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "mailtrap",
	},
	"postmark": {
		Endpoint:      "https://api.postmarkapp.com/email",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "postmark",
	},
	"sparkpost": {
		Endpoint:      "https://api.sparkpost.com/api/v1/transmissions",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "sparkpost",
	},
	"resend": {
		Endpoint:      "https://api.resend.com/emails",
		Method:        http.MethodPost,
		ContentType:   "application/json",
		PayloadFormat: "resend",
	},
	"mailgun": {
		Endpoint:      "https://api.mailgun.net/v3",
		Method:        http.MethodPost,
		ContentType:   "application/x-www-form-urlencoded",
		PayloadFormat: "mailgun",
	},
}

var httpPayloadBuilders = map[string]payloadBuilder{
	"sendgrid":   buildSendGridPayload,
	"brevo":      buildBrevoPayload,
	"sendinblue": buildBrevoPayload,
	"mailtrap":   buildMailtrapPayload,
	"sesv2":      buildSESPayload,
	"ses":        buildSESPayload,
	"aws_ses":    buildSESPayload,
	"amazon_ses": buildSESPayload,
	"postmark":   buildPostmarkPayload,
	"sparkpost":  buildSparkPostPayload,
	"resend":     buildResendPayload,
	"mailgun":    buildMailgunPayload,
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
		"subject":          cfg.Subject,
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
		"text":    fallbackBody(cfg.TextBody),
		"html":    cfg.HTMLBody,
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

func buildBrevoPayload(cfg *EmailConfig) (any, string, error) {
	fromName, fromEmail := splitAddress(cfg.From)
	sender := singleAddressMap(simpleAddress{Name: fromName, Email: fromEmail}, "email", "name")
	payload := map[string]any{
		"sender":      sender,
		"to":          addressMaps(parseAddressList(cfg.To), "email", "name"),
		"subject":     cfg.Subject,
		"textContent": fallbackBody(cfg.TextBody),
		"htmlContent": cfg.HTMLBody,
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
		attachments := make([]map[string]string, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]string{
				"name":    att.Filename,
				"content": att.Content,
			}
			if att.Inline {
				entry["disposition"] = "inline"
				if att.ContentID != "" {
					entry["contentId"] = att.ContentID
				}
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
		payload["TextBody"] = fallbackBody(cfg.TextBody)
	}
	if cfg.HTMLBody != "" {
		payload["HtmlBody"] = cfg.HTMLBody
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
			if att.Inline {
				entry["ContentDisposition"] = "inline"
			}
			attachments = append(attachments, entry)
		}
		payload["Attachments"] = attachments
	}
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
	content := map[string]any{
		"from":    map[string]string{"email": cfg.From, "name": cfg.FromName},
		"subject": cfg.Subject,
		"text":    fallbackBody(cfg.TextBody),
	}
	if cfg.HTMLBody != "" {
		content["html"] = cfg.HTMLBody
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
	if len(cfg.Tags) > 0 {
		var tags []string
		for k := range cfg.Tags {
			tags = append(tags, k)
		}
		sort.Strings(tags)
		payload["metadata"] = cfg.Tags
		payload["description"] = strings.Join(tags, ",")
	}
	return payload, "application/json", nil
}

func buildResendPayload(cfg *EmailConfig) (any, string, error) {
	payload := map[string]any{
		"from":    cfg.From,
		"to":      cfg.To,
		"subject": cfg.Subject,
		"text":    fallbackBody(cfg.TextBody),
		"html":    cfg.HTMLBody,
	}
	if len(cfg.CC) > 0 {
		payload["cc"] = cfg.CC
	}
	if len(cfg.BCC) > 0 {
		payload["bcc"] = cfg.BCC
	}
	if reply := firstAddressEntry(cfg.ReplyTo); reply.Email != "" {
		payload["reply_to"] = []string{reply.Email}
	}
	encoded, err := encodeAllAttachments(cfg)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > 0 {
		attachments := make([]map[string]string, 0, len(encoded))
		for _, att := range encoded {
			entry := map[string]string{
				"filename":     att.Filename,
				"content":      att.Content,
				"content_type": att.MIMEType,
			}
			if att.ContentID != "" {
				entry["cid"] = att.ContentID
			}
			if att.Inline {
				entry["disposition"] = "inline"
			}
			attachments = append(attachments, entry)
		}
		payload["attachments"] = attachments
	}
	return payload, "application/json", nil
}

func buildMailgunPayload(cfg *EmailConfig) (any, string, error) {
	if len(cfg.Attachments) > 0 {
		return nil, "", errors.New("mailgun http builder does not support attachments; use SMTP or raw payload")
	}
	domain := strings.TrimSpace(firstString(cfg.AdditionalData, "domain", "mailgun_domain"))
	if domain == "" {
		domain = inferMailgunDomain(cfg.Endpoint)
	}
	if domain == "" {
		return nil, "", errors.New("mailgun domain is required (set 'domain' in payload)")
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
		form.Set("text", fallbackBody(cfg.TextBody))
	}
	if cfg.HTMLBody != "" {
		form.Set("html", cfg.HTMLBody)
	}
	return form, "application/x-www-form-urlencoded", nil
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
