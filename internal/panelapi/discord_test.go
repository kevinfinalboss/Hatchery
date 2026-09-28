package panelapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidDiscordWebhook(t *testing.T) {
	for _, ok := range []string{
		"https://discord.com/api/webhooks/1479990043794739271/abc_DEF-123",
		"https://discordapp.com/api/webhooks/1/x",
		"https://ptb.discord.com/api/webhooks/1/x",
		"https://canary.discord.com/api/webhooks/1/x",
	} {
		if !validDiscordWebhook(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{
		"", "http://discord.com/api/webhooks/1/x", "https://discord.com.evil.io/api/webhooks/1/x",
		"https://evil.io/api/webhooks/1/x", "https://discord.com:8443/api/webhooks/1/x",
		"https://user@discord.com/api/webhooks/1/x", "https://discord.com/api/webhooks/abc/x",
		"https://discord.com/api/webhooks/1/x?wait=true", "https://discord.com/api/webhooks/1/x#f",
		"https://discord.com/api/webhooks/1/x/extra", "https://162.159.135.232/api/webhooks/1/x",
		"https://discord.com/api/v10/webhooks/1/x",
	} {
		if validDiscordWebhook(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func testDiscord(t *testing.T, handler func(w http.ResponseWriter, n int32)) (*discordHTTP, string, *atomic.Int32, chan map[string]any) {
	t.Helper()
	var calls atomic.Int32
	bodies := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies <- body
		handler(w, n)
	}))
	t.Cleanup(srv.Close)
	return &discordHTTP{Client: srv.Client(), skipValidation: true, retryWait: 10 * time.Millisecond}, srv.URL, &calls, bodies
}

func TestDiscordSendsEmbed(t *testing.T) {
	d, url, calls, bodies := testDiscord(t, func(w http.ResponseWriter, _ int32) { w.WriteHeader(http.StatusNoContent) })
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	err := d.Send(context.Background(), url, discordMessage{Title: "Backup falhou", Description: "mc", URL: "https://p/x", Color: colorRed, At: at})
	if err != nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
	embeds, _ := (<-bodies)["embeds"].([]any)
	e, _ := embeds[0].(map[string]any)
	if e["title"] != "Backup falhou" || e["url"] != "https://p/x" || e["color"] != float64(colorRed) || e["timestamp"] != "2026-09-27T12:00:00Z" {
		t.Fatalf("embed = %v", e)
	}
}

func TestDiscordRetriesOnceOn429And5xx(t *testing.T) {
	for _, first := range []int{http.StatusTooManyRequests, http.StatusBadGateway} {
		d, url, calls, _ := testDiscord(t, func(w http.ResponseWriter, n int32) {
			if n == 1 {
				if first == http.StatusTooManyRequests {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(first)
					_, _ = w.Write([]byte(`{"retry_after": 0.01}`))
					return
				}
				w.WriteHeader(first)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		if err := d.Send(context.Background(), url, discordMessage{Title: "x"}); err != nil || calls.Load() != 2 {
			t.Fatalf("status %d: err=%v calls=%d", first, err, calls.Load())
		}
	}
}

func TestDiscordDoesNotRetryClientErrors(t *testing.T) {
	d, url, calls, _ := testDiscord(t, func(w http.ResponseWriter, _ int32) { w.WriteHeader(http.StatusNotFound) })
	if err := d.Send(context.Background(), url, discordMessage{Title: "x"}); err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestDiscordRefusesInvalidURL(t *testing.T) {
	d := newDiscordHTTP()
	if err := d.Send(context.Background(), "https://evil.io/api/webhooks/1/x", discordMessage{Title: "x"}); err == nil {
		t.Fatal("must refuse a non-Discord URL")
	}
}
