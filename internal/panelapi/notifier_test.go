package panelapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/mailer"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

const testWebhook = "https://discord.com/api/webhooks/1/test-token"

// fakeDiscord records what would have been posted.
type fakeDiscord struct {
	mu   sync.Mutex
	sent []discordMessage
	urls []string
	err  error
}

func (f *fakeDiscord) Send(_ context.Context, url string, m discordMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	f.urls = append(f.urls, url)
	return nil
}

func (f *fakeDiscord) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

var notifyNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func failedBackup(name, uid string, age time.Duration) *v1.GameServerBackup {
	at := metav1.NewTime(notifyNow.Add(-age))
	b := &v1.GameServerBackup{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOrgNS(), UID: types.UID(uid)}}
	b.Spec.GameServerRef.Name = "mc"
	b.Status.Phase = v1.GameServerBackupPhaseFailed
	b.Status.CompletionTime = &at
	return b
}

// notifierFixture: org testorg with its fixture owner, an admin, an admin who switched e-mail
// off and a member; a Discord webhook; a failed backup 5 min ago and one 20 min ago.
func notifierFixture(t *testing.T, objs ...*v1.GameServerBackup) (*Server, *fakeDiscord, *mailer.Recorder, *paneldb.Org) {
	t.Helper()
	gs := &v1.GameServer{ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testOrgNS()}, Spec: v1.GameServerSpec{DisplayName: "Survival"}}
	all := []client.Object{activeTenant(testOrgSlug), gs}
	for _, o := range objs {
		all = append(all, o)
	}
	srv := newTestServer(t, all...)
	ctx := context.Background()
	newMemberToken(t, srv, "adm", paneldb.RoleAdmin)
	newMemberToken(t, srv, "quiet", paneldb.RoleAdmin)
	newMemberToken(t, srv, "mem", paneldb.RoleMember)
	quiet, _ := srv.DB.GetUserByUsername(ctx, "quiet")
	_ = srv.DB.SetNotifyEmail(ctx, quiet.ID, false)
	org, _ := srv.DB.GetOrgBySlug(ctx, testOrgSlug)
	if err := srv.DB.SetDiscordWebhook(ctx, org.ID, testWebhook); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscord{}
	rec := &mailer.Recorder{}
	srv.Discord = discord
	srv.Mailer = rec
	srv.PublicURL = "https://panel.example.com"
	srv.Notified = panelcache.NewMemoryNotifiedStore()
	srv.now = func() time.Time { return notifyNow }
	srv.background = func(f func()) { f() }
	return srv, discord, rec, org
}

func TestNotifyOnceSendsRecentOccurrencesOnce(t *testing.T) {
	srv, discord, rec, _ := notifierFixture(t,
		failedBackup("mc-new", "b-new", 5*time.Minute),
		failedBackup("mc-old", "b-old", 20*time.Minute))
	srv.NotifyOnce(context.Background())
	srv.NotifyOnce(context.Background()) // a second round must not announce it again

	if discord.count() != 1 {
		t.Fatalf("discord messages = %d, want 1 (only the recent failure, once)", discord.count())
	}
	m := discord.sent[0]
	if discord.urls[0] != testWebhook || !strings.Contains(m.Title, "Survival") || !strings.Contains(m.Description, "mc-new") ||
		m.URL != "https://panel.example.com/orgs/testorg/servers/mc" || m.Color != colorRed {
		t.Fatalf("discord message = %+v", m)
	}
	var to []string
	for _, e := range rec.Sent() {
		to = append(to, e.To)
	}
	slices.Sort(to)
	if !slices.Equal(to, []string{"adm@example.com", "fixture-owner@example.com"}) {
		t.Fatalf("e-mails to %v, want the owner and the admin who kept alerts on", to)
	}
	events, _ := srv.DB.ListAudit(context.Background(), testOrgSlug, 50, 0)
	found := false
	for _, e := range events {
		if e.Action == "notification.sent" && e.TargetName == "mc" && strings.Contains(e.Metadata, `"event":"backup.failed"`) {
			found = true
			if strings.Contains(e.Metadata, "test-token") {
				t.Fatal("the webhook URL leaked into the audit")
			}
		}
	}
	if !found {
		t.Fatalf("no notification.sent audit event: %+v", events)
	}
}

func TestNotifyOnceSkipsMutedEvents(t *testing.T) {
	srv, discord, rec, org := notifierFixture(t, failedBackup("mc-new", "b-new", time.Minute))
	_ = srv.DB.SetMutedEvents(context.Background(), org.ID, []string{eventBackupFailed})
	srv.NotifyOnce(context.Background())
	if discord.count() != 0 || len(rec.Sent()) != 0 {
		t.Fatal("a muted event must not be sent")
	}
}

func TestSuspendNotifies(t *testing.T) {
	srv, discord, _, _ := notifierFixture(t)
	token := adminToken(t, srv)
	rec := doRequest(t, srv, http.MethodPost, orgURL("/gameservers/mc/suspend"), token, map[string]string{"reason": "unpaid invoice"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("suspend = %d %s", rec.Code, rec.Body)
	}
	if discord.count() != 1 || !strings.Contains(discord.sent[0].Description, "unpaid invoice") || discord.sent[0].Color != colorOrange {
		t.Fatalf("discord = %+v", discord.sent)
	}
}
