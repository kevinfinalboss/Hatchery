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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerCheckOrigin(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins []string
		origin         string
		want           bool
	}{
		{
			name:           "no Origin header is always allowed (non-browser clients never send one)",
			allowedOrigins: []string{"https://panel.example.com"},
			origin:         "",
			want:           true,
		},
		{
			name:           "unconfigured allowlist keeps today's permissive default",
			allowedOrigins: nil,
			origin:         "https://anything.example.com",
			want:           true,
		},
		{
			name:           "origin present in the allowlist is accepted",
			allowedOrigins: []string{"https://panel.example.com", "http://localhost:5173"},
			origin:         "http://localhost:5173",
			want:           true,
		},
		{
			name:           "origin absent from a configured allowlist is rejected",
			allowedOrigins: []string{"https://panel.example.com"},
			origin:         "https://evil.example.com",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{AllowedOrigins: tt.allowedOrigins}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/gameservers/default/foo/console", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if got := s.checkOrigin(req); got != tt.want {
				t.Errorf("checkOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}
