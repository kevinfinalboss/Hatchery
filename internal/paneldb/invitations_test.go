package paneldb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInvitationReplaceOnReinvite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password1", false)
	o, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)

	_, first, err := s.CreateInvitation(ctx, o.ID, "Guest@Example.com", RoleMember, "en", owner.ID, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	inv, second, err := s.CreateInvitation(ctx, o.ID, "guest@example.com", RoleAdmin, "en", owner.ID, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Role != RoleAdmin || inv.Email != "guest@example.com" {
		t.Fatalf("replaced invitation = %+v", inv)
	}
	if _, err := s.GetInvitationByToken(ctx, first); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("first link still valid: %v", err)
	}
	got, err := s.GetInvitationByToken(ctx, second)
	if err != nil || got.OrgSlug != "acme" || got.InvitedBy != "owner" {
		t.Fatalf("GetInvitationByToken = %+v, %v", got, err)
	}
	list, _ := s.ListInvitations(ctx, o.ID)
	if len(list) != 1 {
		t.Fatalf("list = %v, want exactly one", list)
	}
}

func TestAcceptInvitationNewUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password1", false)
	o, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	_, tok, _ := s.CreateInvitation(ctx, o.ID, "guest@example.com", RoleMember, "", owner.ID, time.Hour)

	if _, _, err := s.AcceptInvitationNewUser(ctx, tok, "owner", "password1", ""); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("taken username: err = %v", err)
	}
	u, inv, err := s.AcceptInvitationNewUser(ctx, tok, "guest", "password1", "Guest Person")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "guest@example.com" || u.DisplayName != "Guest Person" || inv.OrgSlug != "acme" {
		t.Fatalf("user = %+v, inv = %+v", u, inv)
	}
	if role, err := s.GetMembership(ctx, o.ID, u.ID); err != nil || role != RoleMember {
		t.Fatalf("membership = %q, %v", role, err)
	}
	if _, _, err := s.AcceptInvitationNewUser(ctx, tok, "guest2", "password1", ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second accept: err = %v, want ErrInvalidToken", err)
	}
}

func TestAcceptInvitationConcurrentOnlyOneAccountCreated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password1", false)
	o, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	_, tok, _ := s.CreateInvitation(ctx, o.ID, "guest@example.com", RoleMember, "", owner.ID, time.Hour)

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := s.AcceptInvitationNewUser(ctx, tok, "guest"+string(rune('a'+i)), "password1", ""); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins = %d, want 1", wins)
	}
}

func TestAcceptInvitationExistingUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password1", false)
	guest, _ := s.CreateUser(ctx, "guest", "guest@example.com", "password1", false)
	o, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	_, tok, _ := s.CreateInvitation(ctx, o.ID, "GUEST@example.com", RoleAdmin, "", owner.ID, time.Hour)

	if _, err := s.AcceptInvitationExistingUser(ctx, tok, owner.ID); !errors.Is(err, ErrWrongAccount) {
		t.Fatalf("other account: err = %v, want ErrWrongAccount", err)
	}
	inv, err := s.AcceptInvitationExistingUser(ctx, tok, guest.ID)
	if err != nil || inv.Role != RoleAdmin {
		t.Fatalf("accept = %+v, %v", inv, err)
	}
	if role, _ := s.GetMembership(ctx, o.ID, guest.ID); role != RoleAdmin {
		t.Fatalf("role = %q", role)
	}
}

func TestExpiredInvitationIsListedButNotUsable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "password1", false)
	o, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	_, tok, _ := s.CreateInvitation(ctx, o.ID, "guest@example.com", RoleMember, "", owner.ID, -time.Minute)
	if _, err := s.GetInvitationByToken(ctx, tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired: %v", err)
	}
	list, _ := s.ListInvitations(ctx, o.ID)
	if len(list) != 1 || !list[0].Expired {
		t.Fatalf("list = %+v, want one expired", list)
	}
}
