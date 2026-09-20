package paneldb

import (
	"context"
	"errors"
	"testing"
)

func TestValidSlug(t *testing.T) {
	for _, ok := range []string{"acme", "a", "my-org-1", "a1"} {
		if !ValidSlug(ok) {
			t.Errorf("ValidSlug(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Acme", "-acme", "acme-", "a_b", "system", "catalog", "has space",
		"a-name-that-is-way-too-long-for-a-slug"} {
		if ValidSlug(bad) {
			t.Errorf("ValidSlug(%q) = true, want false", bad)
		}
	}
}

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		have, min Role
		want      bool
	}{
		{RoleOwner, RoleAdmin, true}, {RoleAdmin, RoleAdmin, true}, {RoleMember, RoleAdmin, false},
		{RoleMember, RoleMember, true}, {RoleAdmin, RoleOwner, false}, {Role("bogus"), RoleMember, false},
	}
	for _, c := range cases {
		if got := c.have.AtLeast(c.min); got != c.want {
			t.Errorf("%q.AtLeast(%q) = %v, want %v", c.have, c.min, got, c.want)
		}
	}
}

func mustUser(t *testing.T, s *Store, name string) *User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), name, "password", false)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCreateOrgMakesOwnerAndRejectsDuplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := mustUser(t, s, "owner1")

	org, err := s.CreateOrg(ctx, "acme", "Acme Games", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.GetMembership(ctx, org.ID, owner.ID)
	if err != nil || role != RoleOwner {
		t.Fatalf("creator membership = (%q, %v), want owner", role, err)
	}
	if _, err := s.CreateOrg(ctx, "acme", "Again", owner.ID); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate slug: got %v, want ErrAlreadyExists", err)
	}
	if _, err := s.CreateOrg(ctx, "Bad Slug", "x", owner.ID); !errors.Is(err, ErrInvalidSlug) {
		t.Fatalf("invalid slug: got %v, want ErrInvalidSlug", err)
	}
}

func TestLastOwnerInvariant(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := mustUser(t, s, "o")
	other := mustUser(t, s, "p")
	org, _ := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	if err := s.AddMember(ctx, org.ID, other.ID, RoleMember); err != nil {
		t.Fatal(err)
	}

	if err := s.SetMemberRole(ctx, org.ID, owner.ID, RoleAdmin); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demoting the last owner: got %v, want ErrLastOwner", err)
	}
	if err := s.RemoveMember(ctx, org.ID, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("removing the last owner: got %v, want ErrLastOwner", err)
	}

	if err := s.SetMemberRole(ctx, org.ID, other.ID, RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, org.ID, owner.ID); err != nil {
		t.Fatalf("removing an owner when another exists: %v", err)
	}
}

func TestDeleteUserRefusesSoleOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := mustUser(t, s, "sole")
	if _, err := s.CreateOrg(ctx, "acme", "Acme", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("deleting a sole owner: got %v, want ErrLastOwner", err)
	}
}

func TestListOrgsForUserAndMembers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mustUser(t, s, "a")
	b := mustUser(t, s, "b")
	o1, _ := s.CreateOrg(ctx, "one", "One", a.ID)
	if _, err := s.CreateOrg(ctx, "two", "Two", b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, o1.ID, b.ID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, o1.ID, b.ID, RoleAdmin); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("re-adding a member: got %v, want ErrAlreadyExists", err)
	}

	orgs, err := s.ListOrgsForUser(ctx, b.ID)
	if err != nil || len(orgs) != 2 {
		t.Fatalf("ListOrgsForUser(b) = %v, %v; want 2 orgs", orgs, err)
	}
	members, err := s.ListMembers(ctx, o1.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListMembers = %v, %v; want 2", members, err)
	}
}

func TestDeleteOrgCascadesMemberships(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mustUser(t, s, "u")
	org, _ := s.CreateOrg(ctx, "gone", "Gone", u.ID)
	if err := s.DeleteOrg(ctx, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOrgBySlug(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if _, err := s.GetMembership(ctx, org.ID, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("membership should be gone with the org, got %v", err)
	}
	if err := s.DeleteOrg(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice: got %v, want ErrNotFound", err)
	}
}
