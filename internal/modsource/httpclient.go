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

package modsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// requestTimeout bounds every call to a catalog.
const requestTimeout = 10 * time.Second

type httpClient struct {
	base      string
	userAgent string
	header    http.Header
	c         *http.Client
}

func newHTTPClient(base, userAgent string, header http.Header) httpClient {
	return httpClient{base: base, userAgent: userAgent, header: header, c: &http.Client{Timeout: requestTimeout}}
}

func (h httpClient) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	u := h.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return h.do(ctx, http.MethodGet, u, nil, out)
}

func (h httpClient) postJSON(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return h.do(ctx, http.MethodPost, h.base+path, b, out)
}

func (h httpClient) do(ctx context.Context, method, u string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", h.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range h.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := h.c.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if secs <= 0 {
			secs, _ = strconv.Atoi(resp.Header.Get("X-Ratelimit-Reset"))
		}
		if secs <= 0 {
			secs = 60
		}
		return &RateLimitedError{RetryAfter: time.Duration(secs) * time.Second}
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: %s answered %d", ErrUnavailable, req.URL.Host, resp.StatusCode)
	case resp.StatusCode >= 300:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s answered %d: %s", req.URL.Host, resp.StatusCode, msg)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decoding %s: %v", ErrUnavailable, req.URL.Host, err)
	}
	return nil
}
