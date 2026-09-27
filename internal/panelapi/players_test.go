package panelapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

type failingPlayers struct{}

func (failingPlayers) Set(context.Context, string, string, panelcache.PlayersSnapshot) error {
	return nil
}
func (failingPlayers) GetMany(context.Context, string, []string) (map[string]panelcache.PlayersSnapshot, error) {
	return nil, errors.New("valkey down")
}

func playersFixture(t *testing.T) (*Server, string) {
	mc := &gameserversv1alpha1.EggQuery{Protocol: gameserversv1alpha1.EggQueryMinecraft, Port: "game"}
	srv := newTestServer(t,
		queryEgg("paper", gameserversv1alpha1.CatalogNamespace, mc),
		queryEgg("terraria", gameserversv1alpha1.CatalogNamespace, nil),
		phaseServer("up", "paper", gameserversv1alpha1.EggScopeCatalog, gameserversv1alpha1.GameServerPhaseRunning),
		phaseServer("stopping", "paper", gameserversv1alpha1.EggScopeCatalog, gameserversv1alpha1.GameServerPhaseStopping),
		phaseServer("tr", "terraria", gameserversv1alpha1.EggScopeCatalog, gameserversv1alpha1.GameServerPhaseRunning),
	)
	store := panelcache.NewMemoryPlayersStore()
	srv.Players = store
	ctx := context.Background()
	at := time.Now()
	_ = store.Set(ctx, testOrgNS(), "up", panelcache.PlayersSnapshot{Online: 2, Max: 20, Players: []string{"Kevin", "Alex"}, At: at})
	// a server that just stopped still has a live key for up to 45s
	_ = store.Set(ctx, testOrgNS(), "stopping", panelcache.PlayersSnapshot{Online: 1, Max: 20, At: at})
	return srv, adminToken(t, srv)
}

type playersListItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Players *struct {
		Online int      `json:"online"`
		Max    int      `json:"max"`
		Names  []string `json:"names"`
	} `json:"players"`
}

func TestListAttachesPlayerCountsOnlyWhileRunning(t *testing.T) {
	srv, token := playersFixture(t)
	rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers"), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Items []playersListItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	byName := map[string]playersListItem{}
	for _, it := range out.Items {
		byName[it.Metadata.Name] = it
	}
	if len(byName) != 3 {
		t.Fatalf("items = %v", byName)
	}
	if p := byName["up"].Players; p == nil || p.Online != 2 || p.Max != 20 || p.Names != nil {
		t.Fatalf("up players = %+v (names must stay out of the list)", p)
	}
	if byName["stopping"].Players != nil {
		t.Fatal("a server that is not Running must not show players")
	}
	if byName["tr"].Players != nil {
		t.Fatal("no snapshot, no players")
	}
}

type playersDetail struct {
	QueryEnabled bool `json:"queryEnabled"`
	Players      *struct {
		Online int      `json:"online"`
		Names  []string `json:"names"`
		At     string   `json:"at"`
	} `json:"players"`
}

func TestDetailHasNamesAndQueryEnabled(t *testing.T) {
	srv, token := playersFixture(t)
	get := func(name string) (int, playersDetail) {
		var out playersDetail
		rec := doRequest(t, srv, http.MethodGet, orgURL("/gameservers/"+name), token, nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := get("up")
	if code != http.StatusOK || !out.QueryEnabled || out.Players == nil || len(out.Players.Names) != 2 || out.Players.At == "" {
		t.Fatalf("up: code %d, %+v", code, out)
	}
	if _, out = get("stopping"); !out.QueryEnabled || out.Players != nil {
		t.Fatalf("stopping: %+v", out)
	}
	if _, out = get("tr"); out.QueryEnabled || out.Players != nil {
		t.Fatalf("terraria: %+v", out)
	}
}

func TestPlayersStoreFailureNeverBreaksTheResponse(t *testing.T) {
	srv, token := playersFixture(t)
	srv.Players = failingPlayers{}
	for _, path := range []string{"/gameservers", "/gameservers/up"} {
		rec := doRequest(t, srv, http.MethodGet, orgURL(path), token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code %d", path, rec.Code)
		}
	}
}
