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
	"errors"
	"regexp"
	"time"
)

// Role is a user's role inside one organization.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

var roleRank = map[Role]int{RoleMember: 1, RoleAdmin: 2, RoleOwner: 3}

// Valid reports whether r is one of the three known roles.
func (r Role) Valid() bool { return roleRank[r] > 0 }

// AtLeast reports whether r grants at least the power of min. An unknown role
// is never at least anything.
func (r Role) AtLeast(min Role) bool { return roleRank[r] > 0 && roleRank[r] >= roleRank[min] }

var (
	// ErrInvalidSlug is returned when an org slug is not a valid tenant name.
	ErrInvalidSlug = errors.New("invalid organization slug")
	// ErrLastOwner is returned by any change that would leave an org with no owner.
	ErrLastOwner = errors.New("an organization must keep at least one owner")
)

// slugRE mirrors the CEL rule on the Tenant CRD (api/v1alpha1/tenant_types.go).
var slugRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$`)

// ValidSlug reports whether s can be an org slug. "system" and "catalog" are
// reserved: they would map onto the operator's and the Egg catalog's namespaces.
func ValidSlug(s string) bool { return slugRE.MatchString(s) && s != "system" && s != "catalog" }

// Org is an organization. Its slug doubles as the Tenant name.
type Org struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// OrgWithRole is an Org together with the asking user's role in it.
type OrgWithRole struct {
	Org
	Role Role `json:"role"`
}

// Member is one user's membership in an org, with the profile fields other
// members may see. Email is blanked by the Panel API for callers below admin.
type Member struct {
	UserID            int64  `json:"userId"`
	Username          string `json:"username"`
	Role              Role   `json:"role"`
	DisplayName       string `json:"displayName"`
	Email             string `json:"email,omitempty"`
	Discord           string `json:"discord"`
	MinecraftUsername string `json:"minecraftUsername"`
	SteamID           string `json:"steamId"`
}

// CreateOrg inserts the org and, when ownerUserID is not 0, makes that user its
// first owner, atomically. An org without members is valid: platform admins
// are effective owners of every org, and an owner invitation fills it later.
func (s *Store) CreateOrg(ctx context.Context, slug, name string, ownerUserID int64) (*Org, error) {
	if !ValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	o := &Org{Slug: slug, Name: name}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES ($1, $2) RETURNING id, created_at`,
		slug, name).Scan(&o.ID, &o.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	if ownerUserID != 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`, o.ID, ownerUserID); err != nil {
			return nil, err
		}
	}
	return o, tx.Commit()
}

// GetOrgBySlug returns ErrNotFound if there is no such org.
func (s *Store) GetOrgBySlug(ctx context.Context, slug string) (*Org, error) {
	o := &Org{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at FROM organizations WHERE slug = $1`, slug).
		Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

// ListOrgsForUser returns every org userID belongs to, with their role.
func (s *Store) ListOrgsForUser(ctx context.Context, userID int64) ([]OrgWithRole, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.slug, o.name, o.created_at, m.role
		FROM organizations o JOIN memberships m ON m.org_id = o.id
		WHERE m.user_id = $1 ORDER BY o.slug`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgWithRole
	for rows.Next() {
		var o OrgWithRole
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt, &o.Role); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListAllOrgs returns every org (for platform admins).
func (s *Store) ListAllOrgs(ctx context.Context) ([]Org, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, created_at FROM organizations ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Org
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteOrg removes the org and, via ON DELETE CASCADE, its memberships.
func (s *Store) DeleteOrg(ctx context.Context, slug string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM organizations WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetMembership returns userID's role in orgID, or ErrNotFound.
func (s *Store) GetMembership(ctx context.Context, orgID, userID int64) (Role, error) {
	var r Role
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return r, err
}

// ListMembers returns an org's members ordered by username.
func (s *Store) ListMembers(ctx context.Context, orgID int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, m.role, u.display_name, u.email, u.discord, u.minecraft_username, u.steam_id
		FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 ORDER BY u.username`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role, &m.DisplayName, &m.Email, &m.Discord, &m.MinecraftUsername, &m.SteamID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMember adds userID to orgID. ErrAlreadyExists if they are already a member.
func (s *Store) AddMember(ctx context.Context, orgID, userID int64, role Role) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)`, orgID, userID, string(role))
	if isUniqueViolation(err) {
		return ErrAlreadyExists
	}
	return err
}

// SetMemberRole changes a member's role. Demoting the last owner fails with
// ErrLastOwner. The org row is locked for the duration so two concurrent
// changes cannot both see "another owner exists".
func (s *Store) SetMemberRole(ctx context.Context, orgID, userID int64, role Role) error {
	return s.changeMembership(ctx, orgID, userID, func(tx *sql.Tx, current Role) error {
		if current == RoleOwner && role != RoleOwner {
			if err := requireAnotherOwner(ctx, tx, orgID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE memberships SET role = $3 WHERE org_id = $1 AND user_id = $2`, orgID, userID, string(role))
		return err
	})
}

// RemoveMember removes a member. Removing the last owner fails with ErrLastOwner.
func (s *Store) RemoveMember(ctx context.Context, orgID, userID int64) error {
	return s.changeMembership(ctx, orgID, userID, func(tx *sql.Tx, current Role) error {
		if current == RoleOwner {
			if err := requireAnotherOwner(ctx, tx, orgID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE org_id = $1 AND user_id = $2`, orgID, userID)
		return err
	})
}

func (s *Store) changeMembership(ctx context.Context, orgID, userID int64, apply func(*sql.Tx, Role) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if _, err := tx.ExecContext(ctx, `SELECT id FROM organizations WHERE id = $1 FOR UPDATE`, orgID); err != nil {
		return err
	}
	var current Role
	if err := tx.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := apply(tx, current); err != nil {
		return err
	}
	return tx.Commit()
}

// requireAnotherOwner fails with ErrLastOwner unless the org has at least two owners.
func requireAnotherOwner(ctx context.Context, tx *sql.Tx, orgID int64) error {
	var owners int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM memberships WHERE org_id = $1 AND role = 'owner'`, orgID).Scan(&owners); err != nil {
		return err
	}
	if owners < 2 {
		return ErrLastOwner
	}
	return nil
}
