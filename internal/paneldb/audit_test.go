package paneldb

import (
	"context"
	"testing"
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
