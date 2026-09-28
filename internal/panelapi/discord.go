package panelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// discordHosts are the only hosts a webhook may point at: the Panel runs inside the cluster, so
// posting to any URL an org admin typed would be an SSRF.
var discordHosts = map[string]bool{"discord.com": true, "discordapp.com": true, "ptb.discord.com": true, "canary.discord.com": true}

var discordWebhookPath = regexp.MustCompile(`^/api/webhooks/[0-9]+/[A-Za-z0-9_-]+$`)

// validDiscordWebhook accepts only https://<discord host>/api/webhooks/<id>/<token>.
func validDiscordWebhook(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !discordHosts[u.Host] {
		return false
	}
	return u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery && discordWebhookPath.MatchString(u.Path)
}

const (
	colorRed    = 0xE5484D
	colorOrange = 0xF5A524
)

// discordMessage is one alert rendered as a Discord embed.
type discordMessage struct {
	Title       string
	Description string
	URL         string
	Color       int
	At          time.Time
}

// DiscordSender posts a message to a webhook. Swapped for a fake in tests.
type DiscordSender interface {
	Send(ctx context.Context, webhookURL string, m discordMessage) error
}

type discordHTTP struct {
	Client *http.Client
	// skipValidation lets tests post to an httptest server; production always validates.
	skipValidation bool
	// retryWait is the pause before retrying a 5xx (a 429 waits what Discord asks, up to 5s).
	retryWait time.Duration
}

// NewDiscordSender is the production sender: it only posts to validated Discord webhook URLs.
func NewDiscordSender() DiscordSender { return newDiscordHTTP() }

func newDiscordHTTP() *discordHTTP {
	return &discordHTTP{Client: &http.Client{Timeout: 10 * time.Second}, retryWait: time.Second}
}

const maxDiscordRetryAfter = 5 * time.Second

func (d *discordHTTP) Send(ctx context.Context, webhookURL string, m discordMessage) error {
	if !d.skipValidation && !validDiscordWebhook(webhookURL) {
		return errors.New("not a Discord webhook URL")
	}
	embed := map[string]any{"title": m.Title, "description": m.Description, "color": m.Color}
	if m.URL != "" {
		embed["url"] = m.URL
	}
	if !m.At.IsZero() {
		embed["timestamp"] = m.At.UTC().Format(time.RFC3339)
	}
	body, err := json.Marshal(map[string]any{"username": "Hatchery", "embeds": []any{embed}})
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		wait, err := d.post(ctx, webhookURL, body)
		if err == nil || wait < 0 || attempt == 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// post sends one request. It returns how long to wait before retrying, or -1 when the error is
// not worth a retry.
func (d *discordHTTP) post(ctx context.Context, webhookURL string, body []byte) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.Client.Do(req)
	if err != nil {
		return d.retryWait, fmt.Errorf("discord: %w", redactURLError(err))
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return 0, nil
	case res.StatusCode == http.StatusTooManyRequests:
		var rl struct {
			RetryAfter float64 `json:"retry_after"`
		}
		wait := time.Second
		if json.Unmarshal(raw, &rl) == nil && rl.RetryAfter > 0 {
			wait = time.Duration(rl.RetryAfter * float64(time.Second))
		} else if s, err := strconv.ParseFloat(res.Header.Get("Retry-After"), 64); err == nil {
			wait = time.Duration(s * float64(time.Second))
		}
		return min(wait, maxDiscordRetryAfter), errors.New("discord: rate limited")
	case res.StatusCode >= 500:
		return d.retryWait, fmt.Errorf("discord: HTTP %d", res.StatusCode)
	default:
		return -1, fmt.Errorf("discord: HTTP %d", res.StatusCode)
	}
}

// redactURLError drops the URL from an *url.Error: the webhook URL is a credential and must not
// reach logs or the audit trail.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
