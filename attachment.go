package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func loadAttachment(att Attachment) ([]byte, string, string, error) {
	source := strings.TrimSpace(att.Source)
	if source == "" {
		return nil, "", "", errors.New("attachment source is empty")
	}
	if strings.HasPrefix(source, "data:") {
		return decodeDataURI(source, att)
	}
	if looksLikeURL(source) {
		data, name, err := downloadFile(source)
		if err != nil {
			return nil, "", "", err
		}
		mimeType := att.MIMEType
		if mimeType == "" {
			mimeType = detectMIMEType(name, data)
		}
		return data, name, mimeType, nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, "", "", err
	}
	filename := att.Name
	if filename == "" {
		filename = filepath.Base(source)
	}
	mimeType := att.MIMEType
	if mimeType == "" {
		mimeType = detectMIMEType(filename, data)
	}
	return data, filename, mimeType, nil
}

func decodeDataURI(uri string, att Attachment) ([]byte, string, string, error) {
	parts := strings.SplitN(uri, ",", 2)
	if len(parts) != 2 {
		return nil, "", "", fmt.Errorf("invalid data URI for attachment")
	}
	meta := parts[0]
	dataPart := parts[1]
	var data []byte
	var err error
	if strings.HasSuffix(meta, ";base64") {
		data, err = base64.StdEncoding.DecodeString(dataPart)
		if err != nil {
			return nil, "", "", err
		}
	} else {
		decoded, decErr := url.QueryUnescape(dataPart)
		if decErr != nil {
			return nil, "", "", decErr
		}
		data = []byte(decoded)
	}
	mimeType := att.MIMEType
	if mimeType == "" {
		mimeType = strings.TrimPrefix(meta, "data:")
		mimeType = strings.TrimSuffix(mimeType, ";base64")
	}
	name := att.Name
	if name == "" {
		name = "attachment.bin"
	}
	return data, name, mimeType, nil
}

func encodeAttachment(att Attachment) (map[string]string, error) {
	data, filename, mimeType, err := loadAttachment(att)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"filename":     filename,
		"content":      base64.StdEncoding.EncodeToString(data),
		"content_type": mimeType,
	}, nil
}

func encodeAllAttachments(cfg *EmailConfig) ([]encodedAttachment, error) {
	if len(cfg.Attachments) == 0 {
		return nil, nil
	}
	result := make([]encodedAttachment, 0, len(cfg.Attachments))
	for _, att := range cfg.Attachments {
		data, filename, mimeType, err := loadAttachment(att)
		if err != nil {
			return nil, err
		}
		result = append(result, encodedAttachment{
			Filename:  filename,
			MIMEType:  mimeType,
			Content:   base64.StdEncoding.EncodeToString(data),
			Inline:    att.Inline,
			ContentID: att.ContentID,
		})
	}
	return result, nil
}

func partitionAttachments(list []Attachment) (inline []Attachment, regular []Attachment) {
	for _, att := range list {
		if att.Inline {
			inline = append(inline, att)
			continue
		}
		regular = append(regular, att)
	}
	return inline, regular
}

func detectMIMEType(filename string, data []byte) string {
	if ext := filepath.Ext(filename); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	if len(data) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(data)
}

func downloadFile(link string) ([]byte, string, error) {
	resp, err := http.Get(link)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to download %s: %s", link, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	filename := filenameFromURL(link)
	if disp := resp.Header.Get("Content-Disposition"); disp != "" {
		if name := parseFilenameFromDisposition(disp); name != "" {
			filename = name
		}
	}
	return data, filename, nil
}

func filenameFromURL(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return "attachment"
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) == 0 {
		return "attachment"
	}
	if segments[len(segments)-1] == "" {
		return "attachment"
	}
	return segments[len(segments)-1]
}

func parseFilenameFromDisposition(header string) string {
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "filename=") {
			return strings.Trim(part[len("filename="):], "\"")
		}
	}
	return ""
}

func getAttachments(norm *normalizedConfig, canonical string) ([]Attachment, error) {
	val, ok := norm.pullValue(canonical)
	if !ok || val == nil {
		return nil, nil
	}
	switch v := val.(type) {
	case string:
		return []Attachment{{Source: strings.TrimSpace(v)}}, nil
	case []any:
		attachments := make([]Attachment, 0, len(v))
		for _, item := range v {
			att, err := normalizeAttachmentItem(item)
			if err != nil {
				return nil, err
			}
			if att.Source != "" {
				attachments = append(attachments, att)
			}
		}
		return attachments, nil
	case map[string]any:
		var attachments []Attachment
		for _, item := range v {
			att, err := normalizeAttachmentItem(item)
			if err != nil {
				return nil, err
			}
			if att.Source != "" {
				attachments = append(attachments, att)
			}
		}
		return attachments, nil
	default:
		att, err := normalizeAttachmentItem(v)
		return []Attachment{att}, err
	}
}

func normalizeAttachmentItem(item any) (Attachment, error) {
	switch v := item.(type) {
	case string:
		return Attachment{Source: strings.TrimSpace(v)}, nil
	case map[string]any:
		att := Attachment{}
		if source := firstString(v, "source", "path", "file", "filepath", "url"); source != "" {
			att.Source = source
		}
		if name := firstString(v, "name", "filename", "label"); name != "" {
			att.Name = name
		}
		if mime := firstString(v, "content_type", "mimetype", "mime"); mime != "" {
			att.MIMEType = mime
		}
		if cid := firstString(v, "cid", "content_id"); cid != "" {
			att.ContentID = cid
		}
		if inlineRaw, ok := v["inline"]; ok {
			att.Inline = normalizeBool(inlineRaw)
		}
		if att.Source == "" {
			return att, errors.New("attachment entry missing source")
		}
		return att, nil
	default:
		return Attachment{}, fmt.Errorf("unsupported attachment format %T", item)
	}
}

func writeAttachmentPart(msg *strings.Builder, att Attachment, boundary string, inline bool) error {
	data, filename, mimeType, err := loadAttachment(att)
	if err != nil {
		return err
	}
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString(fmt.Sprintf("Content-Type: %s\r\n", mimeType))
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	msg.WriteString(fmt.Sprintf("Content-Disposition: %s; filename=\"%s\"\r\n", disposition, filename))
	if inline {
		cid := att.ContentID
		if cid == "" {
			cid = filename
		}
		if !strings.HasPrefix(cid, "<") {
			cid = "<" + cid + ">"
		}
		msg.WriteString(fmt.Sprintf("Content-ID: %s\r\n", cid))
	}
	msg.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		msg.WriteString(encoded[i:end])
		msg.WriteString("\r\n")
	}
	msg.WriteString("\r\n")
	return nil
}
