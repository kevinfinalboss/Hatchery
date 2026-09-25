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
	"strings"
	"testing"
)

func TestRenderEveryKindInBothLocales(t *testing.T) {
	d := Data{Link: "https://panel.example.com/invite#token=abc", OrgName: "Acme", Role: "admin",
		InvitedBy: "kevin", Username: "kevin", NewEmail: "new@example.com"}
	for _, kind := range []Kind{KindInvitation, KindPasswordReset, KindEmailChange, KindEmailChangeNotice} {
		for _, loc := range []string{"pt-BR", "en"} {
			m, err := Render(kind, loc, d)
			if err != nil {
				t.Fatalf("%s/%s: %v", kind, loc, err)
			}
			if m.Subject == "" || m.Text == "" || m.HTML == "" {
				t.Fatalf("%s/%s: empty part: %+v", kind, loc, m)
			}
			if kind != KindEmailChangeNotice && (!strings.Contains(m.Text, d.Link) || !strings.Contains(m.HTML, "https://panel.example.com/invite#token=abc")) {
				t.Errorf("%s/%s: link missing", kind, loc)
			}
		}
	}
}

func TestRenderFallsBackToPortuguese(t *testing.T) {
	a, _ := Render(KindPasswordReset, "", Data{Link: "x"})
	b, _ := Render(KindPasswordReset, "pt-BR", Data{Link: "x"})
	if a.Subject != b.Subject {
		t.Fatalf("empty locale subject = %q, want pt-BR %q", a.Subject, b.Subject)
	}
}

func TestRenderEscapesHTML(t *testing.T) {
	m, err := Render(KindInvitation, "en", Data{Link: "https://x", OrgName: "<script>alert(1)</script>"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "<script>") {
		t.Fatal("org name was not escaped in HTML")
	}
}
