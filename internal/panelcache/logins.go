package panelcache

import (
	"context"
	"time"
)

// LoginLimits configures the failed-login limiter. Two counters exist because
// limiting by username alone would let anyone lock out another person's
// account by failing their login on purpose; the user+IP pair cannot be
// abused that way, and the per-IP ceiling catches one IP trying many usernames.
type LoginLimits struct {
	PerIP     int64
	PerUserIP int64
	Window    time.Duration
}

// DefaultLoginLimits: 20 failures per IP or 5 per username+IP, per 15 minutes.
var DefaultLoginLimits = LoginLimits{PerIP: 20, PerUserIP: 5, Window: 15 * time.Minute}

// LoginLimiter rate-limits failed logins. Only failures count.
type LoginLimiter interface {
	// Blocked reports whether this attempt should be refused, and for how long.
	Blocked(ctx context.Context, username, ip string) (blocked bool, retryAfter time.Duration, err error)
	RecordFailure(ctx context.Context, username, ip string) error
	// RecordSuccess resets the username+IP counter (not the per-IP one).
	RecordSuccess(ctx context.Context, username, ip string) error
}
