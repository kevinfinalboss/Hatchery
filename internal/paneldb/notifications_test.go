package paneldb

import (
	"context"
	"slices"
	"testing"
)

func TestNotificationSettings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password", false)
	org, err := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetNotificationSettings(ctx, org.ID)
	if err != nil || got.DiscordWebhookURL != "" || len(got.Muted) != 0 {
		t.Fatalf("default settings = %+v, %v", got, err)
	}
	const url = "https://discord.com/api/webhooks/1/abc"
	if err := s.SetDiscordWebhook(ctx, org.ID, url); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMutedEvents(ctx, org.ID, []string{"schedule.failed", "backup.failed"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetNotificationSettings(ctx, org.ID)
	slices.Sort(got.Muted)
	if got.DiscordWebhookURL != url || !slices.Equal(got.Muted, []string{"backup.failed", "schedule.failed"}) {
		t.Fatalf("settings = %+v", got)
	}
	// Setting again updates in place; muting replaces the whole list.
	if err := s.SetMutedEvents(ctx, org.ID, []string{"gameserver.suspended"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDiscordWebhook(ctx, org.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetNotificationSettings(ctx, org.ID)
	if got.DiscordWebhookURL != "" || !slices.Equal(got.Muted, []string{"gameserver.suspended"}) {
		t.Fatalf("after update = %+v", got)
	}

	if err := s.DeleteOrg(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM org_notification_muted`).Scan(&n)
	if n != 0 {
		t.Fatalf("muted rows survived the org: %d", n)
	}
}

func TestAlertRecipientsAndNotifyEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password", false)
	admin, _ := s.CreateUser(ctx, "admin", "admin@example.com", "password", false)
	quiet, _ := s.CreateUser(ctx, "quiet", "quiet@example.com", "password", false)
	member, _ := s.CreateUser(ctx, "member", "member@example.com", "password", false)
	org, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	_ = s.AddMember(ctx, org.ID, admin.ID, RoleAdmin)
	_ = s.AddMember(ctx, org.ID, quiet.ID, RoleAdmin)
	_ = s.AddMember(ctx, org.ID, member.ID, RoleMember)
	_ = s.UpdateProfile(ctx, admin.ID, Profile{Locale: "en"})

	u, _ := s.GetUser(ctx, quiet.ID)
	if !u.NotifyEmail {
		t.Fatal("notify_email must default to true")
	}
	if err := s.SetNotifyEmail(ctx, quiet.ID, false); err != nil {
		t.Fatal(err)
	}
	if u, _ = s.GetUser(ctx, quiet.ID); u.NotifyEmail {
		t.Fatal("SetNotifyEmail(false) did not stick")
	}

	got, err := s.ListAlertRecipients(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.Username+"/"+r.Email+"/"+r.Locale)
	}
	if !slices.Equal(names, []string{"admin/admin@example.com/en", "owner/owner@example.com/"}) {
		t.Fatalf("recipients = %v", names)
	}
}
