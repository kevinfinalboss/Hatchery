package panelcache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryPlayersStoreExpiresAndIsPerServer(t *testing.T) {
	now := time.Now()
	s := NewMemoryPlayersStore()
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	_ = s.Set(ctx, "ns", "mc", PlayersSnapshot{Online: 3, Max: 20, Players: []string{"Kevin"}, At: now})

	got, err := s.GetMany(ctx, "ns", []string{"mc", "other"})
	if err != nil || len(got) != 1 || got["mc"].Online != 3 || got["mc"].Players[0] != "Kevin" {
		t.Fatalf("GetMany = %+v, %v", got, err)
	}
	now = now.Add(PlayersTTL + time.Second)
	if got, _ := s.GetMany(ctx, "ns", []string{"mc"}); len(got) != 0 {
		t.Fatalf("expired snapshot still returned: %+v", got)
	}
}
