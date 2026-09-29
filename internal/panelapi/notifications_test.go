package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type notificationsJSON struct {
	DiscordConfigured bool     `json:"discordConfigured"`
	Muted             []string `json:"muted"`
	Events            []string `json:"events"`
}

func getNotifications(t *testing.T, srv *Server, token string) (int, notificationsJSON, string) {
	t.Helper()
	rec := doRequest(t, srv, http.MethodGet, orgURL("/notifications"), token, nil)
	var out notificationsJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func TestNotificationSettingsAPI(t *testing.T) {
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)

	code, got, _ := getNotifications(t, srv, admin)
	if code != http.StatusOK || got.DiscordConfigured || len(got.Muted) != 0 || len(got.Events) != 5 {
		t.Fatalf("default = %d %+v", code, got)
	}

	put := func(body map[string]any) int {
		return doRequest(t, srv, http.MethodPut, orgURL("/notifications"), admin, body).Code
	}
	if c := put(map[string]any{"discordWebhookUrl": testWebhook, "muted": []string{"schedule.failed"}}); c != http.StatusOK {
		t.Fatalf("put = %d", c)
	}
	_, got, raw := getNotifications(t, srv, admin)
	if !got.DiscordConfigured || len(got.Muted) != 1 || strings.Contains(raw, "test-token") {
		t.Fatalf("after put = %+v / %s (the URL must never come back)", got, raw)
	}
	if c := put(map[string]any{"muted": []string{}}); c != http.StatusOK { // URL omitted: kept
		t.Fatalf("put without url = %d", c)
	}
	if _, got, _ = getNotifications(t, srv, admin); !got.DiscordConfigured || len(got.Muted) != 0 {
		t.Fatalf("omitting the URL must keep it: %+v", got)
	}
	if c := put(map[string]any{"discordWebhookUrl": "", "muted": []string{}}); c != http.StatusOK {
		t.Fatalf("remove = %d", c)
	}
	if _, got, _ = getNotifications(t, srv, admin); got.DiscordConfigured {
		t.Fatal(`"" must remove the webhook`)
	}
	if c := put(map[string]any{"discordWebhookUrl": "https://discord.com.evil.io/api/webhooks/1/x", "muted": []string{}}); c != http.StatusUnprocessableEntity {
		t.Fatalf("bad url = %d, want 422", c)
	}
	if c := put(map[string]any{"muted": []string{"gameserver.exploded"}}); c != http.StatusUnprocessableEntity {
		t.Fatalf("unknown event = %d, want 422", c)
	}

	events, _ := srv.DB.ListAudit(context.Background(), testOrgSlug, 50, 0)
	for _, e := range events {
		if strings.Contains(e.Metadata, "test-token") {
			t.Fatalf("webhook URL in audit: %+v", e)
		}
	}

	member := newMemberToken(t, srv, "mem", paneldb.RoleMember)
	if c, _, _ := getNotifications(t, srv, member); c != http.StatusForbidden {
		t.Fatalf("member = %d, want 403", c)
	}
	outsider := newUserToken(t, srv, "outsider", false)
	if c, _, _ := getNotifications(t, srv, outsider); c != http.StatusNotFound {
		t.Fatalf("non-member = %d, want 404", c)
	}
}

func TestNotificationTestEndpoint(t *testing.T) {
	srv := newTestServer(t)
	admin := newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	newMemberToken(t, srv, "other", paneldb.RoleAdmin)
	discord := &fakeDiscord{}
	mail := &mailer.Recorder{}
	srv.Discord, srv.Mailer, srv.PublicURL = discord, mail, "https://panel.example.com"
	srv.Notified = panelcache.NewMemoryNotifiedStore()

	test := func() (int, map[string]string) {
		rec := doRequest(t, srv, http.MethodPost, orgURL("/notifications/test"), admin, nil)
		var out map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if c, out := test(); c != http.StatusOK || out["discord"] != "notConfigured" || out["email"] != "sent" {
		t.Fatalf("without webhook = %d %v", c, out)
	}
	_ = doRequest(t, srv, http.MethodPut, orgURL("/notifications"), admin, map[string]any{"discordWebhookUrl": testWebhook, "muted": []string{}})
	if c, out := test(); c != http.StatusOK || out["discord"] != "sent" || out["email"] != "sent" {
		t.Fatalf("with webhook = %d %v", c, out)
	}
	for _, m := range mail.Sent() {
		if m.To != "adm@example.com" {
			t.Fatalf("test e-mail went to %q, only the caller must get it", m.To)
		}
	}
	for i := 0; i < 3; i++ {
		test()
	}
	if c, _ := test(); c != http.StatusTooManyRequests {
		t.Fatalf("6th test in a minute = %d, want 429", c)
	}
}

func TestNotifyEmailOnMe(t *testing.T) {
	srv := newTestServer(t)
	token := newUserToken(t, srv, "kevin", false)
	rec := doRequest(t, srv, http.MethodPatch, "/api/v1/me", token, map[string]any{"notifyEmail": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body)
	}
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil)
	var me struct {
		NotifyEmail *bool `json:"notifyEmail"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.NotifyEmail == nil || *me.NotifyEmail {
		t.Fatalf("me = %s", rec.Body)
	}
}
