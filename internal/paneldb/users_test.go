/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package paneldb

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateUserEmailIsUniqueAndLowercased(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "kevin", "Kevin@Example.COM", "password1", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "kevin@example.com" {
		t.Fatalf("email = %q, want lowercased", u.Email)
	}
	if _, err := s.CreateUser(ctx, "other", "KEVIN@example.com", "password1", false); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("same e-mail in other case: err = %v, want ErrEmailTaken", err)
	}
	if _, err := s.CreateUser(ctx, "kevin", "new@example.com", "password1", false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("same username: err = %v, want ErrAlreadyExists", err)
	}
}

func TestVerifyPasswordByUsernameOrEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false); err != nil {
		t.Fatal(err)
	}
	for _, login := range []string{"kevin", "kevin@example.com", "KEVIN@Example.com"} {
		if _, err := s.VerifyPassword(ctx, login, "password1"); err != nil {
			t.Errorf("VerifyPassword(%q) = %v, want ok", login, err)
		}
	}
	if _, err := s.VerifyPassword(ctx, "kevin@example.com", "wrong"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong password: err = %v", err)
	}
}

func TestUpdateProfileAndGetUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	p := Profile{DisplayName: "Kevin Gomes", Locale: "pt-BR", TimeZone: "America/Sao_Paulo",
		Discord: "kevin.g", MinecraftUsername: "Kevin_MC", SteamID: "76561197960287930"}
	if err := s.UpdateProfile(ctx, u.ID, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUserByEmail(ctx, "KEVIN@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != p {
		t.Fatalf("profile = %+v, want %+v", got.Profile, p)
	}
}

func TestSetPasswordAndVerifyUserPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	if err := s.SetPassword(ctx, u.ID, "password2"); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyUserPassword(ctx, u.ID, "password1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old password still accepted: %v", err)
	}
	if err := s.VerifyUserPassword(ctx, u.ID, "password2"); err != nil {
		t.Errorf("new password: %v", err)
	}
}

func TestSetEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	s.CreateUser(ctx, "other", "other@example.com", "password1", false)
	if _, err := s.SetEmail(ctx, u.ID, "OTHER@example.com"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("taken: %v", err)
	}
	got, err := s.SetEmail(ctx, u.ID, "New@Example.com")
	if err != nil || got.Email != "new@example.com" {
		t.Fatalf("SetEmail = %+v, %v", got, err)
	}
}

func TestSetAdminKeepsAtLeastOneAdmin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, "a", "a@example.com", "password1", true)
	b, _ := s.CreateUser(ctx, "b", "b@example.com", "password1", false)
	if err := s.SetAdmin(ctx, a.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoting the only admin: err = %v, want ErrLastAdmin", err)
	}
	if err := s.SetAdmin(ctx, b.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdmin(ctx, a.ID, false); err != nil {
		t.Fatalf("demoting with another admin left: %v", err)
	}
}

func TestRevokeUserSessionsExceptCurrent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	keep, _, _ := s.CreateSession(ctx, u.ID, time.Hour)
	drop, _, _ := s.CreateSession(ctx, u.ID, time.Hour)
	if err := s.RevokeUserSessions(ctx, u.ID, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateSession(ctx, keep); err != nil {
		t.Errorf("kept session was revoked: %v", err)
	}
	if _, err := s.ValidateSession(ctx, drop); !errors.Is(err, ErrNotFound) {
		t.Errorf("other session still valid: %v", err)
	}
	if err := s.RevokeUserSessions(ctx, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateSession(ctx, keep); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke-all left a session: %v", err)
	}
}

func TestCreateOrgWithoutOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	o, err := s.CreateOrg(ctx, "acme", "Acme", 0)
	if err != nil {
		t.Fatal(err)
	}
	members, err := s.ListMembers(ctx, o.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("members = %v, %v; want none", members, err)
	}
}

func TestListMembersCarriesProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "kevin", "kevin@example.com", "password1", false)
	_ = s.UpdateProfile(ctx, u.ID, Profile{DisplayName: "Kevin Gomes", Discord: "kevin.g"})
	o, _ := s.CreateOrg(ctx, "acme", "Acme", u.ID)
	members, err := s.ListMembers(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := members[0]
	if m.DisplayName != "Kevin Gomes" || m.Discord != "kevin.g" || m.Email != "kevin@example.com" {
		t.Fatalf("member = %+v", m)
	}
}
