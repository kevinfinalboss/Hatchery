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
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTP accepts one session and records the DATA payload. starttls says
// whether EHLO advertises STARTTLS (it never actually negotiates it).
func fakeSMTP(t *testing.T, starttls bool) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				if starttls {
					w("250-fake")
					w("250 STARTTLS")
				} else {
					w("250 fake")
				}
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				w("250 ok")
			case cmd == "DATA":
				w("354 go ahead")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				got <- b.String()
				w("250 queued")
			case cmd == "QUIT":
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().String(), got
}

func newTestSMTP(t *testing.T, addr, mode string) *SMTPMailer {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	m, err := NewSMTP(SMTPConfig{Host: host, Port: p, From: "Hatchery <noreply@example.com>", TLS: mode, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSMTPSendsMultipartWithEncodedSubject(t *testing.T) {
	addr, got := fakeSMTP(t, false)
	m := newTestSMTP(t, addr, "none")
	err := m.Send(context.Background(), Message{To: "kevin@example.com", Subject: "Convite para a organização",
		Text: "olá, texto", HTML: "<p>olá, html</p>"})
	if err != nil {
		t.Fatal(err)
	}
	data := <-got
	for _, want := range []string{"To: kevin@example.com", "Subject: =?utf-8?q?", "multipart/alternative",
		"Content-Type: text/plain; charset=utf-8", "Content-Type: text/html; charset=utf-8", "Message-Id: <"} {
		if !strings.Contains(data, want) {
			t.Errorf("message missing %q:\n%s", want, data)
		}
	}
}

func TestSMTPStartTLSRequiredButNotOffered(t *testing.T) {
	addr, _ := fakeSMTP(t, false)
	m := newTestSMTP(t, addr, "starttls")
	if err := m.Send(context.Background(), Message{To: "a@example.com", Subject: "s", Text: "t", HTML: "h"}); err == nil {
		t.Fatal("expected an error when the server does not offer STARTTLS")
	}
}

func TestNewSMTPRejectsBadConfig(t *testing.T) {
	for _, cfg := range []SMTPConfig{
		{Host: "", Port: 25, From: "a@example.com", TLS: "none"},
		{Host: "h", Port: 25, From: "not an address", TLS: "none"},
		{Host: "h", Port: 25, From: "a@example.com", TLS: "ssl"},
	} {
		if _, err := NewSMTP(cfg); err == nil {
			t.Errorf("NewSMTP(%+v) accepted", cfg)
		}
	}
}

func TestRecorder(t *testing.T) {
	var r Recorder
	_ = r.Send(context.Background(), Message{To: "a@example.com"})
	if len(r.Sent()) != 1 {
		t.Fatal("recorder did not keep the message")
	}
}
