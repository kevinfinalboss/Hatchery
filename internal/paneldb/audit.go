package paneldb

import (
	"context"
	"database/sql"
	"time"
)

// AuditEvent is one row of the audit log. OrgSlug "" marks a platform-level event.
type AuditEvent struct {
	ID            int64     `json:"id"`
	OrgSlug       string    `json:"orgSlug,omitempty"`
	ActorUserID   *int64    `json:"actorUserId,omitempty"`
	ActorUsername string    `json:"actorUsername"`
	Action        string    `json:"action"`
	TargetType    string    `json:"targetType,omitempty"`
	TargetName    string    `json:"targetName,omitempty"`
	Outcome       string    `json:"outcome"`
	IP            string    `json:"ip,omitempty"`
	Metadata      string    `json:"metadata,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// RecordAudit appends an event. The Store exposes no way to update or delete one.
func (s *Store) RecordAudit(ctx context.Context, e AuditEvent) error {
	org := sql.NullString{String: e.OrgSlug, Valid: e.OrgSlug != ""}
	actor := sql.NullInt64{}
	if e.ActorUserID != nil {
		actor = sql.NullInt64{Int64: *e.ActorUserID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events (org_slug, actor_user_id, actor_username, action, target_type, target_name, outcome, ip, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		org, actor, e.ActorUsername, e.Action, e.TargetType, e.TargetName, e.Outcome, e.IP, e.Metadata)
	return err
}

// ListAudit returns up to limit events for orgSlug (or the platform events when
// orgSlug is ""), newest first. beforeID, when non-zero, returns only events
// with a smaller id, which is how callers page.
func (s *Store) ListAudit(ctx context.Context, orgSlug string, limit int, beforeID int64) ([]AuditEvent, error) {
	org := sql.NullString{String: orgSlug, Valid: orgSlug != ""}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, org_slug, actor_user_id, actor_username, action, target_type, target_name, outcome, ip, metadata, created_at
		FROM audit_events
		WHERE ((($1::text) IS NULL AND org_slug IS NULL) OR org_slug = $1::text)
		  AND (($2::bigint) = 0 OR id < $2::bigint)
		ORDER BY id DESC LIMIT $3`, org, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var orgCol sql.NullString
		var actor sql.NullInt64
		if err := rows.Scan(&e.ID, &orgCol, &actor, &e.ActorUsername, &e.Action, &e.TargetType,
			&e.TargetName, &e.Outcome, &e.IP, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.OrgSlug = orgCol.String
		if actor.Valid {
			v := actor.Int64
			e.ActorUserID = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
