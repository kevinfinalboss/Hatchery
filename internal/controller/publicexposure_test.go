package controller

import "testing"

func TestAllocatePublicPort(t *testing.T) {
	if port, ok := allocatePublicPort(30000, 30002, nil); !ok || port != 30000 {
		t.Fatalf("empty pool: got (%d, %v), want (30000, true)", port, ok)
	}
	used := map[int32]bool{30000: true, 30001: true}
	if port, ok := allocatePublicPort(30000, 30002, used); !ok || port != 30002 {
		t.Fatalf("skip used: got (%d, %v), want (30002, true)", port, ok)
	}
	full := map[int32]bool{30000: true, 30001: true, 30002: true}
	if _, ok := allocatePublicPort(30000, 30002, full); ok {
		t.Fatal("exhausted range must return ok=false")
	}
}
