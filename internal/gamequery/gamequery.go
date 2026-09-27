// Package gamequery asks a running game server how many players are online, using the
// game's own status protocol. It knows nothing about Kubernetes.
package gamequery

import (
	"context"
	"fmt"
)

// MaxPlayers caps how many player names a Result carries.
const MaxPlayers = 50

// Result is what a status query reports.
type Result struct {
	Online  int
	Max     int
	Players []string
}

// Query dispatches on the protocol name used by Egg.spec.query.protocol.
func Query(ctx context.Context, protocol, addr string) (Result, error) {
	switch protocol {
	case "minecraft":
		return Minecraft(ctx, addr)
	case "a2s":
		return A2S(ctx, addr)
	default:
		return Result{}, fmt.Errorf("unknown query protocol %q", protocol)
	}
}

func appendName(names []string, n string) []string {
	if n == "" || len(names) >= MaxPlayers {
		return names
	}
	return append(names, n)
}
