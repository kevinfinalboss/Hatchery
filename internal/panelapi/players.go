package panelapi

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

// playersCountJSON is what the server list shows: no names, to keep the list light.
type playersCountJSON struct {
	Online int `json:"online"`
	Max    int `json:"max"`
}

type playersJSON struct {
	Online int       `json:"online"`
	Max    int       `json:"max"`
	Names  []string  `json:"names"`
	At     time.Time `json:"at"`
}

// playersFor reads the live snapshots of the running servers among gss. Players are only
// shown for Running servers: a server that just stopped keeps a live key for up to
// PlayersTTL. A store error is logged and treated as "no data".
func (s *Server) playersFor(ctx context.Context, namespace string, gss []gameserversv1alpha1.GameServer) map[string]panelcache.PlayersSnapshot {
	if s.Players == nil {
		return nil
	}
	var names []string
	for _, gs := range gss {
		if gs.Status.Phase == gameserversv1alpha1.GameServerPhaseRunning {
			names = append(names, gs.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	snaps, err := s.Players.GetMany(ctx, namespace, names)
	if err != nil {
		logf.FromContext(ctx).Error(err, "reading online players")
		return nil
	}
	return snaps
}

// eggQueryEnabled reports whether the server's Egg declares a usable spec.query. A missing
// Egg is just "no".
func (s *Server) eggQueryEnabled(ctx context.Context, gs *gameserversv1alpha1.GameServer) bool {
	var egg gameserversv1alpha1.Egg
	if err := s.Client.Get(ctx, types.NamespacedName{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}, &egg); err != nil {
		return false
	}
	_, ok := egg.Spec.QueryPort()
	return ok
}
