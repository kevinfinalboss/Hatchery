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

package panelapi

import (
	"context"
	"net/url"
	"strings"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
)

var mailLog = authLog.WithName("mail")

// mailEnabled: e-mail needs both a transport and a public URL to link to.
func (s *Server) mailEnabled() bool { return s.Mailer != nil && s.PublicURL != "" }

func (s *Server) link(page, token string) string {
	return strings.TrimRight(s.PublicURL, "/") + "/" + page + "#token=" + url.QueryEscape(token)
}

// sendMail renders kind in locale and sends it to to.
func (s *Server) sendMail(ctx context.Context, to, locale string, kind mailer.Kind, d mailer.Data) error {
	msg, err := mailer.Render(kind, locale, d)
	if err != nil {
		return err
	}
	msg.To = to
	return s.Mailer.Send(ctx, msg)
}

// runBackground runs f outside the request (tests make it synchronous).
func (s *Server) runBackground(f func()) {
	if s.background != nil {
		s.background(f)
		return
	}
	go f()
}
