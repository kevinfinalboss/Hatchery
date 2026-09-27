package panelapi

import (
	"context"
	"errors"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/gamequery"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

func queryEgg(name, ns string, q *gameserversv1alpha1.EggQuery) *gameserversv1alpha1.Egg {
	return &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: gameserversv1alpha1.EggSpec{
			Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:1"}},
			StartCommand: "run",
			Ports:        []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565, Protocol: corev1.ProtocolTCP}},
			Query:        q,
		},
	}
}

func phaseServer(name, egg string, scope gameserversv1alpha1.EggScope, phase gameserversv1alpha1.GameServerPhase) *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOrgNS()},
		Spec:       gameserversv1alpha1.GameServerSpec{EggRef: gameserversv1alpha1.GameServerEggRef{Name: egg, Scope: scope}},
		Status:     gameserversv1alpha1.GameServerStatus{Phase: phase},
	}
}

func TestPlayersSamplerQueriesOnlyRunningServersWithAQuery(t *testing.T) {
	mc := &gameserversv1alpha1.EggQuery{Protocol: gameserversv1alpha1.EggQueryMinecraft, Port: "game"}
	broken := &gameserversv1alpha1.EggQuery{Protocol: gameserversv1alpha1.EggQueryMinecraft, Port: "rcon"}
	srv := newTestServer(t,
		activeTenant(testOrgSlug),
		queryEgg("paper", gameserversv1alpha1.CatalogNamespace, mc),
		queryEgg("private", testOrgNS(), mc),
		queryEgg("terraria", gameserversv1alpha1.CatalogNamespace, nil),
		queryEgg("broken", testOrgNS(), broken),
		phaseServer("up", "paper", gameserversv1alpha1.EggScopeCatalog, gameserversv1alpha1.GameServerPhaseRunning),
		phaseServer("own", "private", "", gameserversv1alpha1.GameServerPhaseRunning),
		phaseServer("booting", "paper", gameserversv1alpha1.EggScopeCatalog, gameserversv1alpha1.GameServerPhaseStarting),
		phaseServer("noquery", "terraria", gameserversv1alpha1.EggScopeCatalog, gameserversv1alpha1.GameServerPhaseRunning),
		phaseServer("bad", "broken", "", gameserversv1alpha1.GameServerPhaseRunning),
		phaseServer("gone", "missing-egg", "", gameserversv1alpha1.GameServerPhaseRunning),
		phaseServer("down", "private", "", gameserversv1alpha1.GameServerPhaseRunning),
	)
	store := panelcache.NewMemoryPlayersStore()
	var mu sync.Mutex
	var asked []string
	p := &PlayersSampler{
		Client: srv.Client,
		Store:  store,
		Query: func(_ context.Context, protocol, addr string) (gamequery.Result, error) {
			mu.Lock()
			asked = append(asked, protocol+" "+addr)
			mu.Unlock()
			if addr == "down."+testOrgNS()+".svc.cluster.local:25565" {
				return gamequery.Result{}, errors.New("connection refused")
			}
			return gamequery.Result{Online: 2, Max: 20, Players: []string{"Kevin", "Alex"}}, nil
		},
	}
	p.SampleOnce(context.Background())

	if len(asked) != 3 {
		t.Fatalf("asked %v, want up, own and down only", asked)
	}
	for _, a := range asked {
		if a[:len("minecraft ")] != "minecraft " {
			t.Fatalf("protocol not passed through: %q", a)
		}
	}
	got, _ := store.GetMany(context.Background(), testOrgNS(), []string{"up", "own", "down", "booting", "noquery", "bad"})
	if len(got) != 2 || got["up"].Online != 2 || got["own"].Players[1] != "Alex" {
		t.Fatalf("stored %+v", got)
	}
	if got["up"].At.IsZero() {
		t.Fatal("At must be set")
	}
	if p.lastErr != "" {
		t.Fatalf("a failed query is not a sampler error: %q", p.lastErr)
	}
}
