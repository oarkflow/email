package main

import (
	"crypto/tls"
	"fmt"
	"net"
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
