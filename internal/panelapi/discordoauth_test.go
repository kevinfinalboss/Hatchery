package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kevinfinalboss/Hatchery/internal/discord"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// fakeDiscordAPI answers the OAuth2 token exchange and /users/@me. The code picks the answer:
// "user-<id>" logs in Discord user <id>; "guild-<id>" installs the bot in guild <id>.
type fakeDiscordCalls struct {
	mu         sync.Mutex
	registered []string
	edits      []string
}

func (f *fakeDiscordCalls) lastEdit() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.edits) == 0 {
		return ""
	}
	return f.edits[len(f.edits)-1]
}

func fakeDiscordAPI(t *testing.T) (*discord.Client, *fakeDiscordCalls) {
	calls := &fakeDiscordCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			_ = r.ParseForm()
			code := r.Form.Get("code")
			if strings.HasPrefix(code, "guild-") {
				id := strings.TrimPrefix(code, "guild-")
				_, _ = w.Write([]byte(`{"access_token":"t","guild":{"id":"` + id + `","name":"Guild ` + id + `"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"` + code + `"}`))
		case r.URL.Path == "/users/@me":
			id := strings.TrimPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "user-")
			_, _ = w.Write([]byte(`{"id":"` + id + `","username":"du` + id + `"}`))
		case r.Method == http.MethodPut:
			calls.mu.Lock()
			calls.registered = append(calls.registered, r.URL.Path)
			calls.mu.Unlock()
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/webhooks/app/"):
			var body struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			calls.mu.Lock()
			calls.edits = append(calls.edits, body.Content)
			calls.mu.Unlock()
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &discord.Client{AppID: "app", BotToken: "bot", ClientSecret: "s", BaseURL: srv.URL, HTTP: srv.Client()}, calls
}

func botServer(t *testing.T, objs ...client.Object) (*Server, *fakeDiscordCalls) {
	srv := newTestServer(t, objs...)
	client, calls := fakeDiscordAPI(t)
	srv.PublicURL = "https://panel.example.com"
	srv.Bot = &DiscordBot{Client: client}
	srv.background = func(f func()) { f() }
	return srv, calls
}

// startOAuth calls an authorize route and returns the state it put in the URL.
func startOAuth(t *testing.T, srv *Server, method, path, token string) string {
	t.Helper()
	rec := doRequest(t, srv, method, path, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
	}
	var out struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	u, err := url.Parse(out.URL)
	if err != nil || u.Query().Get("redirect_uri") != "https://panel.example.com/api/v1/discord/oauth/callback" {
		t.Fatalf("authorize url = %s", out.URL)
	}
	return u.Query().Get("state")
}

func callback(t *testing.T, srv *Server, state, code string) string {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/discord/oauth/callback?state="+url.QueryEscape(state)+"&code="+code, "", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d %s", rec.Code, rec.Body)
	}
	return rec.Header().Get("Location")
}

func TestDiscordLinkAccount(t *testing.T) {
	srv, _ := botServer(t)
	alice := newUserToken(t, srv, "alice", false)
	bob := newUserToken(t, srv, "bob", false)
	ctx := context.Background()

	st := startOAuth(t, srv, http.MethodGet, "/api/v1/me/discord/authorize", alice)
	if loc := callback(t, srv, st, "user-42"); loc != "https://panel.example.com/account?discord=linked" {
		t.Fatalf("redirect = %s", loc)
	}
	if u, _ := srv.DB.GetUserByDiscordID(ctx, "42"); u == nil || u.Username != "alice" || u.DiscordUsername != "du42" {
		t.Fatalf("link = %+v", u)
	}
	// The state is single use.
	if loc := callback(t, srv, st, "user-42"); !strings.HasSuffix(loc, "discord=error") {
		t.Fatalf("reused state = %s", loc)
	}
	// The same Discord account cannot be linked to a second user.
	st = startOAuth(t, srv, http.MethodGet, "/api/v1/me/discord/authorize", bob)
	if loc := callback(t, srv, st, "user-42"); !strings.HasSuffix(loc, "discord=taken") {
		t.Fatalf("taken = %s", loc)
	}
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/me/discord", alice, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unlink = %d", rec.Code)
	}
	if _, err := srv.DB.GetUserByDiscordID(ctx, "42"); err == nil {
		t.Fatal("still linked")
	}
}

func TestDiscordConnectOrg(t *testing.T) {
	srv, _ := botServer(t)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	ctx := context.Background()

	if rec := doRequest(t, srv, http.MethodPost, orgURL("/discord/authorize"), member, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("member authorize = %d", rec.Code)
	}
	st := startOAuth(t, srv, http.MethodPost, orgURL("/discord/authorize"), admin)
	if loc := callback(t, srv, st, "guild-g1"); loc != "https://panel.example.com/settings?discord=connected" {
		t.Fatalf("redirect = %s", loc)
	}
	if o, err := srv.DB.GetOrgByGuild(ctx, "g1"); err != nil || o.Slug != testOrgSlug {
		t.Fatalf("guild link = %+v %v", o, err)
	}
	rec := doRequest(t, srv, http.MethodGet, orgURL("/discord"), admin, nil)
	if !strings.Contains(rec.Body.String(), `"guildName":"Guild g1"`) {
		t.Fatalf("GET discord = %s", rec.Body)
	}

	// Another org cannot take the same guild.
	other, _ := srv.DB.CreateOrg(ctx, "other", "Other", userNamed(t, srv, "adm").ID)
	_ = other
	st = startOAuth(t, srv, http.MethodPost, "/api/v1/orgs/other/discord/authorize", admin)
	if loc := callback(t, srv, st, "guild-g1"); !strings.HasSuffix(loc, "discord=taken") {
		t.Fatalf("taken guild = %s", loc)
	}

	// Demoted between the click and the callback: refused.
	st = startOAuth(t, srv, http.MethodPost, orgURL("/discord/authorize"), admin)
	org, _ := srv.DB.GetOrgBySlug(ctx, testOrgSlug)
	_ = srv.DB.SetMemberRole(ctx, org.ID, userNamed(t, srv, "adm").ID, paneldb.RoleMember)
	if loc := callback(t, srv, st, "guild-g2"); !strings.HasSuffix(loc, "discord=error") {
		t.Fatalf("demoted = %s", loc)
	}
	if id, _, _ := srv.DB.GetOrgGuild(ctx, org.ID); id != "g1" {
		t.Fatalf("guild changed by a demoted user: %s", id)
	}
}

func TestDiscordConnectPlatform(t *testing.T) {
	srv, registered := botServer(t)
	root := adminToken(t, srv)
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/platform/discord/authorize", newUserToken(t, srv, "joe", false), nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin = %d", rec.Code)
	}
	st := startOAuth(t, srv, http.MethodPost, "/api/v1/platform/discord/authorize", root)
	if loc := callback(t, srv, st, "guild-pg"); loc != "https://panel.example.com/orgs?discord=connected" {
		t.Fatalf("redirect = %s", loc)
	}
	if v, _ := srv.DB.GetSetting(context.Background(), settingPlatformGuild); v != "pg" {
		t.Fatalf("platform guild = %q", v)
	}
	if len(registered.registered) == 0 || registered.registered[len(registered.registered)-1] != "/applications/app/guilds/pg/commands" {
		t.Fatalf("platform commands not registered in the guild: %v", registered.registered)
	}
}

func TestDiscordRoutesOffWithoutConfig(t *testing.T) {
	srv := newTestServer(t)
	token := newUserToken(t, srv, "x", false)
	if rec := doRequest(t, srv, http.MethodGet, "/api/v1/me/discord/authorize", token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unconfigured = %d, want 404", rec.Code)
	}
}
