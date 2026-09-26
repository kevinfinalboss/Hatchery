package panelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

var auditLog = logf.Log.WithName("panelapi-audit")

// forbiddenMetaKeys are substrings that must never appear in an audit
// metadata key: the log records what was done and to what, never a secret or
// the content of a file.
var forbiddenMetaKeys = []string{"password", "token", "ticket", "secret", "content"}

// sanitizeMeta serializes meta as JSON, dropping any key that looks like it
// would hold a credential or file content. It returns "" when nothing is left.
func sanitizeMeta(meta map[string]string) string {
	clean := map[string]string{}
	for k, v := range meta {
		lk := strings.ToLower(k)
		drop := false
		for _, bad := range forbiddenMetaKeys {
			if strings.Contains(lk, bad) {
				drop = true
				break
			}
		}
		if !drop {
			clean[k] = truncate(v, 512)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	b, err := json.Marshal(clean) // map keys are marshalled sorted: stable output
	if err != nil {
		return ""
	}
	return string(b)
}

// writeAudit stores one event. A failure is logged and swallowed: an audit
// outage must not stop people operating their servers. The write uses its own
// short deadline, detached from the request context, so a client that
// disconnects right after its action does not lose the record.
func (s *Server) writeAudit(ctx context.Context, e paneldb.AuditEvent) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	record := s.recordAuditFn
	if record == nil {
		record = s.DB.RecordAudit
	}
	if err := record(ctx, e); err != nil {
		auditLog.Error(err, "failed to record audit event", "action", e.Action, "org", e.OrgSlug)
	}
}

// auditEvent records an event for the authenticated user of the request. Use
// it from handlers that know more than a wrapper could (the new server's
// name, the new org's slug). orgSlug "" makes it a platform-level event.
func (s *Server) auditEvent(r *http.Request, orgSlug, action, targetType, targetName, outcome string, meta map[string]string) {
	s.auditEventAs(r, userFromContext(r.Context()), orgSlug, action, targetType, targetName, outcome, meta)
}

// auditEventAs is auditEvent for an explicit actor — needed at login, where
// the user is not in the request context yet. A nil actor records an unknown
// identity (a failed login): the attempted username is in targetName.
func (s *Server) auditEventAs(r *http.Request, actor *paneldb.User, orgSlug, action, targetType, targetName, outcome string, meta map[string]string) {
	e := paneldb.AuditEvent{
		OrgSlug:       orgSlug,
		ActorUsername: "unknown",
		Action:        action,
		TargetType:    targetType,
		TargetName:    truncate(targetName, maxLoggedUsername),
		Outcome:       outcome,
		IP:            clientIP(r, s.TrustedProxies),
		Metadata:      sanitizeMeta(meta),
	}
	if actor != nil {
		id := actor.ID
		e.ActorUserID = &id
		e.ActorUsername = actor.Username
	}
	s.writeAudit(r.Context(), e)
}

// statusRecorder captures the status a handler wrote, and stays transparent to
// streaming (Flush) and to http.ResponseController (Unwrap).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// audited records an event after the wrapped handler ran, with the outcome
// derived from the HTTP status: 2xx/3xx success, 401/403 denied, anything else
// failed. The target is the GameServer in the URL ({name}). It sits inside
// requireOrgRole, so the org and the actor are already in the context; a
// caller who was refused for lacking the role never reaches it.
func (s *Server) audited(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		outcome := "success"
		switch {
		case rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden:
			outcome = "denied"
		case rec.status >= 400:
			outcome = "failed"
		}
		acc := orgAccessFromContext(r.Context())
		if acc == nil {
			return
		}
		s.auditEvent(r, acc.Org.Slug, action, "gameserver", r.PathValue("name"), outcome, nil)
	})
}

const (
	defaultAuditLimit = 50
	maxAuditLimit     = 200
)

type auditPageResponse struct {
	Events     []paneldb.AuditEvent `json:"events"`
	NextBefore *int64               `json:"nextBefore"`
	// RetentionDays is how long these events are kept; 0 means forever.
	RetentionDays int `json:"retentionDays"`
}

func (s *Server) serveAudit(w http.ResponseWriter, r *http.Request, orgSlug string) {
	limit := defaultAuditLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, maxAuditLimit)
	}
	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "before must be a non-negative integer")
			return
		}
		before = n
	}

	events, err := s.DB.ListAudit(r.Context(), orgSlug, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := auditPageResponse{Events: events}
	if resp.Events == nil {
		resp.Events = []paneldb.AuditEvent{}
	}
	if len(events) == limit {
		last := events[len(events)-1].ID
		resp.NextBefore = &last
	}
	resp.RetentionDays = s.AuditRetentionDays
	if orgSlug != "" {
		var tenant gameserversv1alpha1.Tenant
		if err := s.Client.Get(r.Context(), client.ObjectKey{Name: orgSlug}, &tenant); err == nil && tenant.Spec.Quota.AuditRetentionDays != nil {
			resp.RetentionDays = int(*tenant.Spec.Quota.AuditRetentionDays)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleListOrgAudit lists the org's audit events. Admin or above.
func (s *Server) handleListOrgAudit(w http.ResponseWriter, r *http.Request) {
	s.serveAudit(w, r, orgAccessFromContext(r.Context()).Org.Slug)
}

// handleListPlatformAudit lists platform-level events (logins). Platform admin only.
func (s *Server) handleListPlatformAudit(w http.ResponseWriter, r *http.Request) {
	s.serveAudit(w, r, "")
}
