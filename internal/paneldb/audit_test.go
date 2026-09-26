package paneldb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAuditRecordAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i, action := range []string{"a.one", "a.two", "a.three"} {
		uid := int64(i + 1)
		if err := s.RecordAudit(ctx, AuditEvent{OrgSlug: "acme", ActorUserID: &uid, ActorUsername: "u",
			Action: action, TargetType: "gameserver", TargetName: "gs", Outcome: "success", IP: "1.2.3.4"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordAudit(ctx, AuditEvent{OrgSlug: "other", ActorUsername: "u", Action: "x", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAudit(ctx, AuditEvent{ActorUsername: "nobody", Action: "login.failure", Outcome: "denied"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListAudit(ctx, "acme", 10, 0)
	if err != nil || len(got) != 3 {
		t.Fatalf("ListAudit(acme) = %v, %v; want 3 events", got, err)
	}
	if got[0].Action != "a.three" || got[2].Action != "a.one" {
		t.Errorf("events must come newest first, got %s ... %s", got[0].Action, got[2].Action)
	}

	page, err := s.ListAudit(ctx, "acme", 10, got[0].ID)
	if err != nil || len(page) != 2 || page[0].Action != "a.two" {
		t.Fatalf("cursor page = %v, %v; want the 2 events older than a.three", page, err)
	}

	limited, _ := s.ListAudit(ctx, "acme", 1, 0)
	if len(limited) != 1 {
		t.Fatalf("limit=1 returned %d events", len(limited))
	}

	platform, err := s.ListAudit(ctx, "", 10, 0)
	if err != nil || len(platform) != 1 || platform[0].Action != "login.failure" {
		t.Fatalf("platform events = %v, %v; want only the login.failure", platform, err)
	}
}

func TestAuditSurvivesOrgAndUserDeletion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mustUser(t, s, "actor")
	if _, err := s.CreateOrg(ctx, "temp", "Temp", u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAudit(ctx, AuditEvent{OrgSlug: "temp", ActorUserID: &u.ID, ActorUsername: "actor",
		Action: "org.create", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOrg(ctx, "temp"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAudit(ctx, "temp", 10, 0)
	if err != nil || len(got) != 1 || got[0].ActorUsername != "actor" {
		t.Fatalf("the audit record must outlive the org and the user, got %v, %v", got, err)
	}
}

func TestPruneAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	record := func(org, action string, age time.Duration) {
		if err := s.RecordAudit(ctx, AuditEvent{OrgSlug: org, ActorUsername: "u", Action: action, Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE audit_events SET created_at = $1 WHERE action = $2`, time.Now().Add(-age), action); err != nil {
			t.Fatal(err)
		}
	}
	day := 24 * time.Hour
	record("custom", "custom.old", 40*day)
	record("custom", "custom.new", 10*day)
	record("plain", "plain.old", 400*day)
	record("plain", "plain.new", 40*day)
	record("", "platform.old", 400*day)
	record("gone", "gone.old", 400*day)

	n, err := s.PruneOrgAudit(ctx, "custom", time.Now().Add(-30*day))
	if err != nil || n != 1 {
		t.Fatalf("PruneOrgAudit = %d, %v; want 1", n, err)
	}
	n, err = s.PruneAuditExcept(ctx, time.Now().Add(-365*day), []string{"custom"})
	if err != nil || n != 3 {
		t.Fatalf("PruneAuditExcept = %d, %v; want 3 (plain.old, platform.old, gone.old)", n, err)
	}
	// An org with its own retention is never pruned by the default, even for very old events.
	record("custom", "custom.ancient", 4000*day)
	if n, _ := s.PruneAuditExcept(ctx, time.Now().Add(-365*day), []string{"custom"}); n != 0 {
		t.Fatalf("default retention touched an org with its own: %d", n)
	}
	// No org with its own retention: the default covers every org.
	if n, _ := s.PruneAuditExcept(ctx, time.Now().Add(-3000*day), nil); n != 1 {
		t.Fatalf("empty keep list: %d, want 1 (custom.ancient)", n)
	}

	var left []string
	rows, err := s.db.QueryContext(ctx, `SELECT action FROM audit_events ORDER BY action`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		left = append(left, a)
	}
	if strings.Join(left, ",") != "custom.new,plain.new" {
		t.Fatalf("left = %v", left)
	}
}
