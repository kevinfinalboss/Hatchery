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
	"database/sql"
	"testing"
)

func insertLegacyUser(t *testing.T, ctx context.Context, db *sql.DB, username string) *User {
	t.Helper()
	u := &User{Username: username}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, 'x', false) RETURNING id, created_at`,
		username).Scan(&u.ID, &u.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMemberGrantsRoundTripAndCascade(t *testing.T) {
	s := newTestStore(t) // store_test.go: disposable database per package
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", "owner@example.com", "pw", false)
	m, _ := s.CreateUser(ctx, "mod", "mod@example.com", "pw", false)
	org, err := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, org.ID, m.ID, RoleMember); err != nil {
		t.Fatal(err)
	}

	if g, _ := s.ListMemberGrants(ctx, org.ID, m.ID); len(g) != 0 {
		t.Fatalf("a member added after the migration starts with no grants, got %v", g)
	}

	in := []Grant{
		{GameServer: "mc", Permissions: []Permission{PermPower, PermConsoleRead, PermPower}}, // duplicate on purpose
		{GameServer: AllServers, Permissions: []Permission{PermFilesRead}},
	}
	if err := s.SetMemberGrants(ctx, org.ID, m.ID, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListMemberGrants(ctx, org.ID, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].GameServer != "*" || got[1].GameServer != "mc" ||
		len(got[1].Permissions) != 2 || got[1].Permissions[0] != PermConsoleRead || got[1].Permissions[1] != PermPower {
		t.Fatalf("got %+v", got)
	}

	if err := s.DeleteGameServerGrants(ctx, org.ID, "mc"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListMemberGrants(ctx, org.ID, m.ID); len(got) != 1 || got[0].GameServer != "*" {
		t.Fatalf("after deleting mc's grants: %+v", got)
	}

	if err := s.RemoveMember(ctx, org.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListMemberGrants(ctx, org.ID, m.ID); len(got) != 0 {
		t.Fatalf("removing the member must cascade to their grants: %+v", got)
	}
}

func TestPermissionValid(t *testing.T) {
	if !PermSchedules.Valid() || Permission("files.delete").Valid() {
		t.Fatal("Valid must accept exactly AllPermissions")
	}
}

func TestMigration0004GrantsExistingMembers(t *testing.T) {
	db := newIsolatedTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_init.sql", "0002_organizations.sql", "0003_audit.sql"} {
		if err := applyMigration(ctx, db, name); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(db)

	owner := insertLegacyUser(t, ctx, db, "owner")
	mem := insertLegacyUser(t, ctx, db, "mem")
	adm := insertLegacyUser(t, ctx, db, "adm")
	org, err := s.CreateOrg(ctx, "acme", "Acme", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.AddMember(ctx, org.ID, mem.ID, RoleMember)
	_ = s.AddMember(ctx, org.ID, adm.ID, RoleAdmin)

	if err := applyMigration(ctx, db, "0004_member_permissions.sql"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListMemberGrants(ctx, org.ID, mem.ID)
	want := []Permission{PermConsoleRead, PermConsoleWrite, PermPower, PermFilesRead, PermFilesWrite, PermBackupsRead}
	if len(got) != 1 || got[0].GameServer != AllServers || len(got[0].Permissions) != len(want) {
		t.Fatalf("existing member: %+v, want * with %v", got, want)
	}
	for i := range want {
		if got[0].Permissions[i] != want[i] {
			t.Fatalf("existing member: %+v, want * with %v", got, want)
		}
	}
	if g, _ := s.ListMemberGrants(ctx, org.ID, adm.ID); len(g) != 0 {
		t.Fatalf("admins get no rows (they have everything by role): %+v", g)
	}
	if g, _ := s.ListMemberGrants(ctx, org.ID, owner.ID); len(g) != 0 {
		t.Fatalf("owners get no rows: %+v", g)
	}
}
