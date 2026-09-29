package paneldb

import (
	"context"
	"errors"
	"testing"
)

func TestDiscordUserLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "alice", "alice@example.com", "password", false)
	b, _ := s.CreateUser(ctx, "bob", "bob@example.com", "password", false)

	if err := s.SetDiscordLink(ctx, a.ID, "111", "alice#0"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUserByDiscordID(ctx, "111")
	if err != nil || got.ID != a.ID || got.DiscordUserID != "111" || got.DiscordUsername != "alice#0" {
		t.Fatalf("GetUserByDiscordID = %+v, %v", got, err)
	}
	if err := s.SetDiscordLink(ctx, b.ID, "111", "x"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("linking a taken Discord ID = %v, want ErrAlreadyExists", err)
	}
	// Relinking the same user to a new account replaces it; two unlinked users never collide.
	if err := s.SetDiscordLink(ctx, a.ID, "222", "alice2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUserByDiscordID(ctx, "111"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old ID still linked: %v", err)
	}
	if err := s.ClearDiscordLink(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetUser(ctx, a.ID); u.DiscordUserID != "" || u.DiscordUsername != "" {
		t.Fatalf("after clear: %+v", u)
	}
}

func TestOrgGuildAndSettings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password", false)
	acme, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	beta, _ := s.CreateOrg(ctx, "beta", "Beta", owner.ID)

	if err := s.SetOrgGuild(ctx, acme.ID, "g1", "Acme Discord", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrgGuild(ctx, beta.ID, "g1", "x", owner.ID); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("guild of another org = %v, want ErrAlreadyExists", err)
	}
	if o, err := s.GetOrgByGuild(ctx, "g1"); err != nil || o.Slug != "acme" {
		t.Fatalf("GetOrgByGuild = %+v, %v", o, err)
	}
	// Reconnecting the org to another guild replaces its link.
	if err := s.SetOrgGuild(ctx, acme.ID, "g2", "New", owner.ID); err != nil {
		t.Fatal(err)
	}
	if id, name, err := s.GetOrgGuild(ctx, acme.ID); err != nil || id != "g2" || name != "New" {
		t.Fatalf("GetOrgGuild = %q %q %v", id, name, err)
	}
	if err := s.ClearOrgGuild(ctx, acme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOrgByGuild(ctx, "g2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleared guild still resolves: %v", err)
	}

	if v, err := s.GetSetting(ctx, "k"); err != nil || v != "" {
		t.Fatalf("missing setting = %q, %v", v, err)
	}
	_ = s.SetSetting(ctx, "k", "1")
	_ = s.SetSetting(ctx, "k", "2")
	if v, _ := s.GetSetting(ctx, "k"); v != "2" {
		t.Fatalf("setting = %q", v)
	}
	_ = s.DeleteSetting(ctx, "k")
	if v, _ := s.GetSetting(ctx, "k"); v != "" {
		t.Fatalf("deleted setting = %q", v)
	}
}
