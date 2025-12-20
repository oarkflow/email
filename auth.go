package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strings"
)

func dialPlainClient(cfg *EmailConfig, addr string) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return client, nil
}

func dialTLSClient(cfg *EmailConfig, addr string) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	tlsConfig := &tls.Config{ServerName: cfg.Host, InsecureSkipVerify: cfg.SkipTLSVerify}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return client, nil
}

func buildSMTPAuth(cfg *EmailConfig) (smtp.Auth, error) {
	authType := strings.ToLower(strings.TrimSpace(cfg.SMTPAuth))
	switch authType {
	case "", "plain":
		return smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host), nil
	case "login":
		return &loginAuth{username: cfg.Username, password: cfg.Password, host: cfg.Host}, nil
	case "cram-md5", "crammd5":
		return smtp.CRAMMD5Auth(cfg.Username, cfg.Password), nil
	case "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported smtp auth %s", authType)
	}
}

// loginAuth implements the LOGIN SMTP auth mechanism.
type loginAuth struct {
	username string
	password string
	host     string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server.Name != a.host {
		return "", nil, fmt.Errorf("unexpected server name %s", server.Name)
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch strings.ToLower(string(fromServer)) {
		case "username:", "user:":
			return []byte(a.username), nil
		case "password:", "pass:":
			return []byte(a.password), nil
		default:
			return nil, fmt.Errorf("unexpected login challenge: %s", string(fromServer))
		}
	}
	return nil, nil
}

func applyAuthHeaders(req *http.Request, cfg *EmailConfig, body []byte) {
	token := strings.TrimSpace(cfg.APIToken)
	apiKey := strings.TrimSpace(cfg.APIKey)
	if token == "" {
		token = apiKey
	}

	// Explicit auth override takes priority.
	switch cfg.HTTPAuth {
	case "none":
		return
	case "basic":
		user := cfg.Username
		pass := cfg.Password
		if user != "" || pass != "" {
			req.SetBasicAuth(user, pass)
			return
		}
	case "bearer":
		if token == "" {
			break
		}
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", strings.TrimSpace(cfg.HTTPAuthPrefix+" "+token))
		}
		return
	case "api_key_header":
		header := cfg.HTTPAuthHeader
		if header == "" {
			header = "X-API-Key"
		}
		if token != "" && req.Header.Get(header) == "" {
			req.Header.Set(header, token)
		}
		return
	case "api_key_query":
		param := cfg.HTTPAuthQuery
		if param == "" {
			param = "api_key"
		}
		if token != "" {
			q := req.URL.Query()
			if q.Get(param) == "" {
				q.Set(param, token)
				req.URL.RawQuery = q.Encode()
			}
		}
		return
	case "aws_sigv4":
		if err := signAWSv4(req, body, cfg); err != nil {
			log.Printf("sigv4 signing failed: %v", err)
		}
		return
	}

	switch cfg.Provider {
	case "brevo", "sendinblue":
		if apiKey == "" {
			apiKey = token
		}
		if apiKey == "" || req.Header.Get("api-key") != "" {
			return
		}
		req.Header.Set("api-key", apiKey)
		return
	case "mailgun":
		if token == "" {
			return
		}
		req.SetBasicAuth("api", token)
		return
	case "postmark":
		if token == "" {
			return
		}
		if req.Header.Get("X-Postmark-Server-Token") == "" {
			req.Header.Set("X-Postmark-Server-Token", token)
		}
		return
	case "sparkpost":
		if token == "" {
			return
		}
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", token)
		}
		return
	case "resend":
		if token == "" {
			return
		}
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return
	case "ses", "aws_ses", "amazon_ses":
		if err := signAWSv4(req, body, cfg); err != nil {
			log.Printf("sigv4 signing failed: %v", err)
		}
		return
	}

	if token == "" {
		return
	}
	if req.Header.Get("Authorization") != "" {
		return
	}
	req.Header.Set("Authorization", strings.TrimSpace(cfg.HTTPAuthPrefix+" "+token))
}
