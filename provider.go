package main

import (
	"net/http"
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
