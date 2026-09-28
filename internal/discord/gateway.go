package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Gateway opcodes the presence client uses.
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opPresenceUpdate = 3
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11
	activityWatching = 3
)

// Presence keeps the bot "online" with an activity. It receives no events (intents 0): commands
// arrive over HTTP, so a dropped Gateway only turns the bot's dot grey.
type Presence struct {
	Client *Client
	// GatewayURL overrides the URL from GET /gateway/bot (tests).
	GatewayURL string
	// Activity is the "Watching …" text, asked again every Interval.
	Activity func(ctx context.Context) string
	Interval time.Duration
	// MinBackoff is the first reconnect delay (doubles up to a minute).
	MinBackoff time.Duration
	// OnError reports connection problems (optional).
	OnError func(error)
}

// Run connects and reconnects until ctx is done.
func (p *Presence) Run(ctx context.Context) {
	minBackoff := p.MinBackoff
	if minBackoff <= 0 {
		minBackoff = time.Second
	}
	backoff := minBackoff
	for ctx.Err() == nil {
		start := time.Now()
		err := p.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && p.OnError != nil {
			p.OnError(err)
		}
		if time.Since(start) > time.Minute {
			backoff = minBackoff // a session that lived resets the backoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

type gatewayFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
}

func (p *Presence) presence(ctx context.Context) map[string]any {
	return map[string]any{
		"since": nil, "afk": false, "status": "online",
		"activities": []map[string]any{{"name": p.Activity(ctx), "type": activityWatching}},
	}
}

// session runs one connection: Hello, Identify, heartbeats and presence updates, until the
// connection fails or Discord asks for a reconnect.
func (p *Presence) session(ctx context.Context) error {
	url := p.GatewayURL
	if url == "" {
		var err error
		if url, err = p.Client.GatewayURL(ctx); err != nil {
			return err
		}
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url+sep+"v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("gateway dial: %w", err)
	}
	defer conn.Close()

	var hello gatewayFrame
	if err := conn.ReadJSON(&hello); err != nil || hello.Op != opHello {
		return errors.New("gateway: expected Hello")
	}
	var h struct {
		Interval int `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(hello.D, &h); err != nil || h.Interval <= 0 {
		return errors.New("gateway: bad Hello")
	}

	var writeMu sync.Mutex
	send := func(op int, d any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(map[string]any{"op": op, "d": d})
	}
	if err := send(opIdentify, map[string]any{
		"token": p.Client.BotToken, "intents": 0,
		"properties": map[string]string{"os": "linux", "browser": "hatchery", "device": "hatchery"},
		"presence":   p.presence(ctx),
	}); err != nil {
		return err
	}

	var seq struct {
		sync.Mutex
		v *int64
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = conn.Close() // unblocks ReadJSON
	}()
	go func() {
		beat := time.NewTicker(time.Duration(h.Interval) * time.Millisecond)
		defer beat.Stop()
		interval := p.Interval
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		update := time.NewTicker(interval)
		defer update.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-beat.C:
				seq.Lock()
				s := seq.v
				seq.Unlock()
				if send(opHeartbeat, s) != nil {
					cancel()
					return
				}
			case <-update.C:
				if send(opPresenceUpdate, p.presence(ctx)) != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		var f gatewayFrame
		if err := conn.ReadJSON(&f); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("gateway read: %w", err)
		}
		if f.S != nil {
			seq.Lock()
			seq.v = f.S
			seq.Unlock()
		}
		switch f.Op {
		case opHeartbeat:
			seq.Lock()
			s := seq.v
			seq.Unlock()
			if err := send(opHeartbeat, s); err != nil {
				return err
			}
		case opReconnect, opInvalidSession:
			return nil // reconnect with a fresh Identify
		case opDispatch, opHeartbeatACK:
		}
	}
}
