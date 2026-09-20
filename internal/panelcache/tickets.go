package panelcache

import (
	"context"
	"errors"
	"time"
)

// ErrTicketNotFound is returned by Consume for a ticket that never existed,
// already expired, or was already used — deliberately indistinguishable.
var ErrTicketNotFound = errors.New("ticket not found or expired")

// ConsoleTicket is what a console ticket is bound to. A ticket opens exactly
// this user's console on exactly this org's GameServer, once.
type ConsoleTicket struct {
	UserID     int64  `json:"u"`
	Org        string `json:"o"`
	GameServer string `json:"g"`
}

// TicketStore issues and consumes single-use console tickets.
type TicketStore interface {
	// Issue stores t for ttl and returns the opaque ticket to hand to the client.
	Issue(ctx context.Context, t ConsoleTicket, ttl time.Duration) (string, error)
	// Consume returns what ticket was issued for and invalidates it atomically,
	// so two concurrent handshakes with one ticket cannot both succeed.
	Consume(ctx context.Context, ticket string) (*ConsoleTicket, error)
}
