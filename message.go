package main

import (
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"
)

func buildMessage(cfg *EmailConfig) (string, error) {
	var msg strings.Builder
	fromAddr := mail.Address{Name: cfg.FromName, Address: cfg.From}
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromAddr.String()))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(cfg.To, ", ")))
	if len(cfg.CC) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(cfg.CC, ", ")))
	}
	if len(cfg.ReplyTo) > 0 {
		msg.WriteString(fmt.Sprintf("Reply-To: %s\r\n", strings.Join(cfg.ReplyTo, ", ")))
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", cfg.Subject))
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	msg.WriteString(fmt.Sprintf("Message-ID: <%s@%s>\r\n", randomBoundary("msg"), cfg.Host))
	msg.WriteString("MIME-Version: 1.0\r\n")
	if cfg.ReturnPath != "" {
		msg.WriteString(fmt.Sprintf("Return-Path: %s\r\n", cfg.EnvelopeFrom))
	}
	for k, v := range cfg.Headers {
		if strings.EqualFold(k, "Content-Type") {
			continue
		}
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	if len(cfg.ListUnsubscribe) > 0 {
		msg.WriteString(fmt.Sprintf("List-Unsubscribe: %s\r\n", strings.Join(cfg.ListUnsubscribe, ", ")))
		if cfg.ListUnsubscribePost {
			msg.WriteString("List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n")
		}
	}
	if cfg.ConfigurationSet != "" {
		msg.WriteString(fmt.Sprintf("X-SES-CONFIGURATION-SET: %s\r\n", cfg.ConfigurationSet))
	}
	if len(cfg.Tags) > 0 {
		var parts []string
		for k, v := range cfg.Tags {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(parts)
		msg.WriteString(fmt.Sprintf("X-SES-MESSAGE-TAGS: %s\r\n", strings.Join(parts, ";")))
	}

	inline, regular := partitionAttachments(cfg.Attachments)
	if len(regular) > 0 {
		mixedBoundary := randomBoundary("mixed")
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n", mixedBoundary))
		if err := writeBodySection(&msg, cfg, inline, mixedBoundary); err != nil {
			return "", err
		}
		for _, att := range regular {
			if err := writeAttachmentPart(&msg, att, mixedBoundary, false); err != nil {
				return "", err
			}
		}
		msg.WriteString(fmt.Sprintf("--%s--\r\n", mixedBoundary))
		return msg.String(), nil
	}

	if err := writeBodySection(&msg, cfg, inline, ""); err != nil {
		return "", err
	}
	return msg.String(), nil
}

func writeBodySection(msg *strings.Builder, cfg *EmailConfig, inline []Attachment, boundary string) error {
	if boundary != "" {
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	}
	return writeAlternativeBody(msg, cfg, inline)
}

func writeAlternativeBody(msg *strings.Builder, cfg *EmailConfig, inline []Attachment) error {
	hasInline := len(inline) > 0 && cfg.HTMLBody != ""
	if hasInline && cfg.TextBody != "" {
		altBoundary := randomBoundary("alt")
		relatedBoundary := randomBoundary("rel")
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n\r\n", altBoundary))
		msg.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		msg.WriteString(cfg.TextBody)
		msg.WriteString("\r\n\r\n")
		msg.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/related; boundary=%s\r\n\r\n", relatedBoundary))
		msg.WriteString(fmt.Sprintf("--%s\r\n", relatedBoundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		msg.WriteString(cfg.HTMLBody)
		msg.WriteString("\r\n\r\n")
		for _, att := range inline {
			if err := writeAttachmentPart(msg, att, relatedBoundary, true); err != nil {
				return err
			}
		}
		msg.WriteString(fmt.Sprintf("--%s--\r\n", relatedBoundary))
		msg.WriteString(fmt.Sprintf("--%s--\r\n", altBoundary))
		return nil
	}

	if hasInline {
		relatedBoundary := randomBoundary("rel")
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/related; boundary=%s\r\n\r\n", relatedBoundary))
		msg.WriteString(fmt.Sprintf("--%s\r\n", relatedBoundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		msg.WriteString(cfg.HTMLBody)
		msg.WriteString("\r\n\r\n")
		for _, att := range inline {
			if err := writeAttachmentPart(msg, att, relatedBoundary, true); err != nil {
				return err
			}
		}
		msg.WriteString(fmt.Sprintf("--%s--\r\n", relatedBoundary))
		return nil
	}

	if cfg.HTMLBody != "" && cfg.TextBody != "" {
		altBoundary := randomBoundary("alt")
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n\r\n", altBoundary))
		msg.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		msg.WriteString(cfg.TextBody)
		msg.WriteString("\r\n\r\n")
		msg.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		msg.WriteString(cfg.HTMLBody)
		msg.WriteString("\r\n\r\n")
		msg.WriteString(fmt.Sprintf("--%s--\r\n", altBoundary))
		return nil
	}

	contentType := "text/plain"
	body := cfg.TextBody
	if cfg.HTMLBody != "" {
		contentType = "text/html"
		body = cfg.HTMLBody
	}
	msg.WriteString(fmt.Sprintf("Content-Type: %s; charset=UTF-8\r\n\r\n", contentType))
	msg.WriteString(body)
	msg.WriteString("\r\n")
	return nil
}

func gatherRecipients(cfg *EmailConfig) ([]string, error) {
	unique := make(map[string]struct{})
	var recipients []string
	for _, set := range [][]string{cfg.To, cfg.CC, cfg.BCC} {
		for _, candidate := range set {
			_, addr := splitAddress(candidate)
			if addr == "" {
				continue
			}
			addr = strings.ToLower(strings.TrimSpace(addr))
			if addr == "" {
				continue
			}
			if _, exists := unique[addr]; exists {
				continue
			}
			unique[addr] = struct{}{}
			recipients = append(recipients, addr)
		}
	}
	return recipients, nil
}
