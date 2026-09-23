package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// allocatePublicPort returns the lowest port in [min,max] not present in used, or ok=false if
// the whole range is already taken. Deterministic (lowest free port first) so allocation stays
// predictable; ranges here are homelab-sized (at most tens of thousands of ports), so a linear
// scan is not a performance concern.
func allocatePublicPort(min, max int32, used map[int32]bool) (port int32, ok bool) {
	for p := min; p <= max; p++ {
		if !used[p] {
			return p, true
		}
	}
	return 0, false
}

// usedPublicPorts lists every port already claimed by any GameServer's status.publicExposure,
// cluster-wide, so a new allocation never collides with one that is already live.
func usedPublicPorts(ctx context.Context, c client.Reader) (map[int32]bool, error) {
	var list gameserversv1alpha1.GameServerList
	if err := c.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("listing GameServers: %w", err)
	}
	used := make(map[int32]bool)
	for i := range list.Items {
		for _, p := range list.Items[i].Status.PublicExposure.Ports {
			used[p.Port] = true
		}
	}
	return used, nil
}
