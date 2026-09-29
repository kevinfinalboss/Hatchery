package panelcache

import (
	"context"
	"testing"
)

func TestMemoryNotifiedStoreClaimsOnce(t *testing.T) {
	s := NewMemoryNotifiedStore()
	ctx := context.Background()
	if ok, _ := s.Claim(ctx, "acme/backup_failed/1"); !ok {
		t.Fatal("first claim must win")
	}
	if ok, _ := s.Claim(ctx, "acme/backup_failed/1"); ok {
		t.Fatal("second claim must lose")
	}
	if ok, _ := s.Claim(ctx, "acme/backup_failed/2"); !ok {
		t.Fatal("another identity is independent")
	}
}
