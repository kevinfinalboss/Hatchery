package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type gwFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

func TestPresenceGateway(t *testing.T) {
	var mu sync.Mutex
	var frames []string // "conn:op"
	conns := 0
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		mu.Lock()
		conns++
		me := conns
		mu.Unlock()
		_ = c.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 20}})
		for {
			var f gwFrame
			if err := c.ReadJSON(&f); err != nil {
				return
			}
			mu.Lock()
			frames = append(frames, string(rune('0'+me))+":"+string(rune('0'+f.Op)))
			mu.Unlock()
			switch f.Op {
			case 2:
				var id struct {
					Token    string `json:"token"`
					Intents  int    `json:"intents"`
					Presence struct {
						Status     string `json:"status"`
						Activities []struct {
							Name string `json:"name"`
							Type int    `json:"type"`
						} `json:"activities"`
					} `json:"presence"`
				}
				_ = json.Unmarshal(f.D, &id)
				if id.Token != "bot" || id.Intents != 0 || id.Presence.Status != "online" || id.Presence.Activities[0].Name != "3 servers" {
					t.Errorf("identify = %s", f.D)
				}
			case 1:
				_ = c.WriteJSON(map[string]any{"op": 11})
				if me == 1 {
					// Ask the client to reconnect once.
					_ = c.WriteJSON(map[string]any{"op": 7})
				}
			}
		}
	}))
	defer srv.Close()

	p := &Presence{
		Client:     &Client{BotToken: "bot"},
		GatewayURL: "ws" + strings.TrimPrefix(srv.URL, "http"),
		Activity:   func(context.Context) string { return "3 servers" },
		Interval:   30 * time.Millisecond,
		MinBackoff: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	p.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(frames, ",")
	for _, want := range []string{"1:2", "1:1", "2:2", "2:3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
}
