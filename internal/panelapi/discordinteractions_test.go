package panelapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type interactionFixture struct {
	srv   *Server
	calls *fakeDiscordCalls
	priv  ed25519.PrivateKey
}

// interactionSetup: org testorg connected to guild "g1" with server "mc" (stopped, pod running);
// "adm" (org admin) is Discord user 42, "mem" (member, only power on mc) is 43; the platform
// guild is "pg" and "root" (platform admin) is Discord user 99.
func interactionSetup(t *testing.T) *interactionFixture {
	t.Helper()
	srv, calls := botServer(t, fixtureObjects()...)
	pub, priv, _ := ed25519.GenerateKey(nil)
	srv.Bot.PublicKey = pub
	srv.Clientset = k8sfake.NewSimpleClientset()
	srv.botWait = func() {}
	srv.execFn = func(context.Context, string, string, string, []string) (string, error) { return "", nil }
	ctx := context.Background()
	org, _ := srv.DB.GetOrgBySlug(ctx, testOrgSlug)
	newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	newMemberToken(t, srv, "mem", paneldb.RoleMember)
	_ = srv.DB.SetMemberGrants(ctx, org.ID, userNamed(t, srv, "mem").ID, []paneldb.Grant{{GameServer: "mc", Permissions: []paneldb.Permission{paneldb.PermPower}}})
	newUserToken(t, srv, "root", true)
	_ = srv.DB.SetDiscordLink(ctx, userNamed(t, srv, "adm").ID, "42", "adm")
	_ = srv.DB.SetDiscordLink(ctx, userNamed(t, srv, "mem").ID, "43", "mem")
	_ = srv.DB.SetDiscordLink(ctx, userNamed(t, srv, "root").ID, "99", "root")
	_ = srv.DB.SetOrgGuild(ctx, org.ID, "g1", "Acme", userNamed(t, srv, "adm").ID)
	_ = srv.DB.SetSetting(ctx, settingPlatformGuild, "pg")
	return &interactionFixture{srv: srv, calls: calls, priv: priv}
}

func (f *interactionFixture) post(t *testing.T, body map[string]any, sign bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discord/interactions", bytes.NewReader(raw))
	ts := "1700000000"
	sig := hex.EncodeToString(ed25519.Sign(f.priv, append([]byte(ts), raw...)))
	if !sign {
		sig = strings.Repeat("0", 128)
	}
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", ts)
	rec := httptest.NewRecorder()
	f.srv.Routes().ServeHTTP(rec, req)
	return rec
}

type reply struct {
	Type int `json:"type"`
	Data struct {
		Content string `json:"content"`
		Flags   int    `json:"flags"`
		Choices []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"choices"`
	} `json:"data"`
}

func (f *interactionFixture) run(t *testing.T, guild, user, name string, opts map[string]string) reply {
	t.Helper()
	return f.interact(t, 2, guild, user, name, opts, "")
}

func (f *interactionFixture) interact(t *testing.T, typ int, guild, user, name string, opts map[string]string, focused string) reply {
	t.Helper()
	var options []map[string]any
	for k, v := range opts {
		options = append(options, map[string]any{"name": k, "type": 3, "value": v, "focused": k == focused})
	}
	body := map[string]any{"type": typ, "token": "itok", "locale": "pt-BR", "guild_id": guild,
		"member": map[string]any{"user": map[string]any{"id": user}},
		"data":   map[string]any{"name": name, "options": options}}
	if guild == "" {
		delete(body, "guild_id")
		delete(body, "member")
		body["user"] = map[string]any{"id": user}
	}
	rec := f.post(t, body, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d %s", name, rec.Code, rec.Body)
	}
	var r reply
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	return r
}

func TestInteractionSignatureAndPing(t *testing.T) {
	f := interactionSetup(t)
	if rec := f.post(t, map[string]any{"type": 1}, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d", rec.Code)
	}
	rec := f.post(t, map[string]any{"type": 1}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"type":1`) {
		t.Fatalf("ping = %d %s", rec.Code, rec.Body)
	}
}

func TestInteractionContextRefusals(t *testing.T) {
	f := interactionSetup(t)
	if r := f.run(t, "", "42", "servers", nil); !strings.Contains(r.Data.Content, "servidor do Discord") {
		t.Errorf("DM = %q", r.Data.Content)
	}
	if r := f.run(t, "unknown", "42", "servers", nil); !strings.Contains(r.Data.Content, "não está conectado") {
		t.Errorf("unknown guild = %q", r.Data.Content)
	}
	if r := f.run(t, "g1", "777", "servers", nil); !strings.Contains(r.Data.Content, "/account") || r.Data.Flags != 64 {
		t.Errorf("unlinked = %+v", r)
	}
	if r := f.run(t, "pg", "42", "orgs", nil); !strings.Contains(r.Data.Content, "administradores da plataforma") {
		t.Errorf("non-admin in platform guild = %q", r.Data.Content)
	}
}

func TestInteractionOrgCommands(t *testing.T) {
	f := interactionSetup(t)
	ctx := context.Background()
	if r := f.run(t, "g1", "42", "servers", nil); !strings.Contains(r.Data.Content, "`mc`") {
		t.Errorf("servers = %q", r.Data.Content)
	}
	if r := f.run(t, "g1", "42", "start", map[string]string{"server": "mc"}); !strings.Contains(r.Data.Content, "Iniciando") {
		t.Fatalf("start = %q", r.Data.Content)
	}
	var gs v1.GameServer
	_ = f.srv.Client.Get(ctx, types.NamespacedName{Namespace: testOrgNS(), Name: "mc"}, &gs)
	if gs.Spec.State != v1.GameServerStateRunning {
		t.Fatal("start did not change spec.state")
	}
	// The member has power but not console.write: same answer as the panel.
	if r := f.run(t, "g1", "43", "command", map[string]string{"server": "mc", "text": "say hi"}); !strings.Contains(r.Data.Content, "permissão") {
		t.Errorf("member command = %q", r.Data.Content)
	}
	if r := f.run(t, "g1", "43", "stop", map[string]string{"server": "mc"}); !strings.Contains(r.Data.Content, "Parando") {
		t.Errorf("member stop = %q", r.Data.Content)
	}
	r := f.run(t, "g1", "42", "command", map[string]string{"server": "mc", "text": "say hi"})
	if r.Type != 5 || r.Data.Flags != 64 {
		t.Fatalf("command must be deferred and ephemeral: %+v", r)
	}
	if edit := f.calls.lastEdit(); !strings.Contains(edit, "Comando enviado") || !strings.Contains(edit, "fake logs") {
		t.Fatalf("deferred edit = %q", edit)
	}
	events, _ := f.srv.DB.ListAudit(ctx, testOrgSlug, 50, 0)
	found := false
	for _, e := range events {
		if e.Action == "gameserver.command" && e.ActorUsername == "adm" && strings.Contains(e.Metadata, `"via":"discord"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no gameserver.command audit with via=discord")
	}
}

func TestInteractionAutocomplete(t *testing.T) {
	f := interactionSetup(t)
	r := f.interact(t, 4, "g1", "42", "status", map[string]string{"server": "m"}, "server")
	if r.Type != 8 || len(r.Data.Choices) != 1 || r.Data.Choices[0].Value != "mc" {
		t.Fatalf("autocomplete = %+v", r)
	}
	if r := f.interact(t, 4, "g1", "42", "status", map[string]string{"server": "zzz"}, "server"); len(r.Data.Choices) != 0 {
		t.Fatalf("filter = %+v", r.Data.Choices)
	}
	// An unlinked user gets no suggestions, not an error.
	if r := f.interact(t, 4, "g1", "777", "status", map[string]string{"server": ""}, "server"); r.Type != 8 || len(r.Data.Choices) != 0 {
		t.Fatalf("unlinked autocomplete = %+v", r)
	}
}

func TestInteractionPlatformCommands(t *testing.T) {
	f := interactionSetup(t)
	if r := f.run(t, "pg", "99", "orgs", nil); !strings.Contains(r.Data.Content, "`"+testOrgSlug+"`") {
		t.Errorf("orgs = %q", r.Data.Content)
	}
	if r := f.run(t, "pg", "99", "suspend", map[string]string{"org": testOrgSlug, "server": "mc", "reason": "test"}); !strings.Contains(r.Data.Content, "suspenso") {
		t.Fatalf("suspend = %q", r.Data.Content)
	}
	var gs v1.GameServer
	_ = f.srv.Client.Get(context.Background(), types.NamespacedName{Namespace: testOrgNS(), Name: "mc"}, &gs)
	if !gs.Spec.Suspended {
		t.Fatal("not suspended")
	}
	if r := f.run(t, "pg", "99", "health", nil); !strings.Contains(r.Data.Content, "Postgres") {
		t.Errorf("health = %q", r.Data.Content)
	}
	if r := f.interact(t, 4, "pg", "99", "suspend", map[string]string{"org": "test"}, "org"); len(r.Data.Choices) != 1 {
		t.Errorf("org autocomplete = %+v", r.Data.Choices)
	}
}
