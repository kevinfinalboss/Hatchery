/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig configures SMTPMailer. TLS is "starttls" (upgrade after connect,
// the default for port 587), "tls" (TLS from the first byte, port 465) or
// "none" (plain text — only for a local catcher such as Mailpit).
type SMTPConfig struct {
	Host     string
	Port     int
	From     string
	TLS      string
	Username string
	Password string
	Timeout  time.Duration
}

// SMTPMailer sends through one SMTP server, one connection per message.
type SMTPMailer struct {
	cfg  SMTPConfig
	from *mail.Address
}

// NewSMTP validates cfg. Timeout defaults to 10s.
func NewSMTP(cfg SMTPConfig) (*SMTPMailer, error) {
	if cfg.Host == "" || cfg.Port <= 0 {
		return nil, errors.New("smtp host and port are required")
	}
	switch cfg.TLS {
	case "starttls", "tls", "none":
	default:
		return nil, fmt.Errorf("smtp tls must be starttls, tls or none, got %q", cfg.TLS)
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp from %q: %w", cfg.From, err)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &SMTPMailer{cfg: cfg, from: from}, nil
}

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	deadline := time.Now().Add(m.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	if m.cfg.TLS == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connecting to smtp %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if m.cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not offer STARTTLS (set tls to none only for a local catcher)")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp STARTTLS: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(m.from.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	body, err := buildMessage(m.from, msg, time.Now())
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp end of data: %w", err)
	}
	return c.Quit()
}

// buildMessage renders an RFC 5322 multipart/alternative message.
func buildMessage(from *mail.Address, msg Message, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	idBytes := make([]byte, 12)
	_, _ = rand.Read(idBytes)
	domain := "hatchery"
	if at := strings.LastIndex(from.Address, "@"); at >= 0 {
		domain = from.Address[at+1:]
	}

	h := textproto.MIMEHeader{}
	h.Set("From", from.String())
	h.Set("To", msg.To)
	h.Set("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	h.Set("Date", now.Format(time.RFC1123Z))
	h.Set("Message-Id", "<"+hex.EncodeToString(idBytes)+"@"+domain+">")
	h.Set("MIME-Version", "1.0")
	h.Set("Content-Type", "multipart/alternative; boundary="+mw.Boundary())

	var head bytes.Buffer
	for _, k := range []string{"From", "To", "Subject", "Date", "Message-Id", "MIME-Version", "Content-Type"} {
		fmt.Fprintf(&head, "%s: %s\r\n", k, h.Get(k))
	}
	head.WriteString("\r\n")

	for _, part := range []struct{ ctype, body string }{
		{"text/plain; charset=utf-8", msg.Text},
		{"text/html; charset=utf-8", msg.HTML},
	} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), buf.Bytes()...), nil
}
