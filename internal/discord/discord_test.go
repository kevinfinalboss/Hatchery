package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	body := []byte(`{"type":1}`)
	ts := "1700000000"
	sig := hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), body...)))
	if !VerifySignature(pub, sig, ts, body) {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature(pub, sig, ts, []byte(`{"type":2}`)) {
		t.Fatal("tampered body accepted")
	}
	if VerifySignature(pub, sig, "1700000001", body) {
		t.Fatal("changed timestamp accepted")
	}
	if VerifySignature(pub, "zz", ts, body) || VerifySignature(pub, "", ts, body) {
		t.Fatal("garbage signature accepted")
	}
	if key, err := ParsePublicKey(hex.EncodeToString(pub)); err != nil || !key.Equal(pub) {
		t.Fatalf("ParsePublicKey = %v", err)
	}
}

func TestOptionValues(t *testing.T) {
	var in Interaction
	raw := `{"type":2,"guild_id":"9","member":{"user":{"id":"42","username":"kevin"}},"data":{"name":"command","options":[
		{"name":"server","type":3,"value":"mc","focused":true},{"name":"text","type":3,"value":"say hi"}]}}`
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	if in.UserID() != "42" || in.Data.String("server") != "mc" || in.Data.String("text") != "say hi" || in.Data.Focused() != "server" {
		t.Fatalf("parsed %+v", in)
	}
}

func fakeDiscord(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{AppID: "app", BotToken: "bot", ClientSecret: "secret", BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestRegisterCommands(t *testing.T) {
	var got []map[string]any
	var auth, path string
	c := fakeDiscord(t, func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.Method+" "+r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`[]`))
	})
	if err := c.RegisterGuildCommands(context.Background(), "g1", PlatformCommands()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bot bot" || path != "PUT /applications/app/guilds/g1/commands" {
		t.Fatalf("request = %s %s", auth, path)
	}
	names := map[string]map[string]any{}
	for _, c := range got {
		names[c["name"].(string)] = c
	}
	if names["suspend"] == nil || names["health"] == nil {
		t.Fatalf("platform commands = %v", got)
	}
	if loc := names["suspend"]["name_localizations"].(map[string]any)["pt-BR"]; loc != "suspender" {
		t.Fatalf("pt-BR name = %v", loc)
	}
	var org []string
	for _, cmd := range OrgCommands() {
		org = append(org, cmd.Name)
	}
	if strings.Join(org, ",") != "servers,status,start,stop,restart,command,backup" {
		t.Fatalf("org commands = %v", org)
	}
}

func TestExchangeCodeAndMe(t *testing.T) {
	c := fakeDiscord(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			user, pass, _ := r.BasicAuth()
			raw, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(raw))
			if user != "app" || pass != "secret" || form.Get("code") != "abc" || form.Get("redirect_uri") != "https://p/cb" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok","guild":{"id":"g1","name":"Acme"}}`))
		case "/users/@me":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":"42","username":"kevin","global_name":"Kevin"}`))
		}
	})
	tok, err := c.ExchangeCode(context.Background(), "abc", "https://p/cb")
	if err != nil || tok.AccessToken != "tok" || tok.Guild == nil || tok.Guild.ID != "g1" || tok.Guild.Name != "Acme" {
		t.Fatalf("token = %+v, %v", tok, err)
	}
	me, err := c.Me(context.Background(), tok.AccessToken)
	if err != nil || me.ID != "42" || me.Username != "kevin" {
		t.Fatalf("me = %+v, %v", me, err)
	}
	u := c.AuthorizeURL([]string{"bot", "applications.commands"}, "st", "https://p/cb", "0")
	if !strings.Contains(u, "client_id=app") || !strings.Contains(u, "scope=bot+applications.commands") ||
		!strings.Contains(u, "state=st") || !strings.Contains(u, "permissions=0") {
		t.Fatalf("authorize url = %s", u)
	}
}

func TestEditOriginal(t *testing.T) {
	var path, auth string
	var body map[string]any
	c := fakeDiscord(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.Method+" "+r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
	})
	if err := c.EditOriginal(context.Background(), "itok", "done"); err != nil {
		t.Fatal(err)
	}
	if path != "PATCH /webhooks/app/itok/messages/@original" || auth != "" || body["content"] != "done" {
		t.Fatalf("edit = %s auth=%q body=%v", path, auth, body)
	}
}
