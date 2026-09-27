package panelapi

import (
	"context"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/gamequery"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
)

const (
	DefaultPlayersInterval    = 15 * time.Second
	DefaultPlayersTimeout     = 3 * time.Second
	DefaultPlayersConcurrency = 16
)

// PlayerQueryFunc asks one server who is online. Swapped for a fake in tests.
type PlayerQueryFunc func(ctx context.Context, protocol, addr string) (gamequery.Result, error)

// PlayersSampler asks every running GameServer whose Egg declares spec.query how many
// players are online, and keeps the answer in Store for panelcache.PlayersTTL.
type PlayersSampler struct {
	Client      client.Client
	Query       PlayerQueryFunc
	Store       panelcache.PlayersStore
	Interval    time.Duration
	Timeout     time.Duration
	Concurrency int

	lastErr string
}

func NewPlayersSampler(c client.Client, store panelcache.PlayersStore) *PlayersSampler {
	return &PlayersSampler{Client: c, Query: gamequery.Query, Store: store}
}

// Run blocks until ctx is done.
func (p *PlayersSampler) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = DefaultPlayersInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		p.SampleOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type playersTarget struct {
	namespace, name, protocol, addr string
}

// gameServerAddr is the in-cluster address of one of the server's ports (its Service DNS).
func gameServerAddr(gs *gameserversv1alpha1.GameServer, port int32) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", gs.Name, gs.Namespace, port)
}

// SampleOnce runs one round. A query that fails writes nothing: the previous snapshot
// expires on its own. Only sampler-level errors are logged, and only when they change.
func (p *PlayersSampler) SampleOnce(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("players-sampler")
	targets, err := p.targets(ctx)
	if err != nil {
		p.report(logger, err)
		return
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultPlayersTimeout
	}
	conc := p.Concurrency
	if conc <= 0 {
		conc = DefaultPlayersConcurrency
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var storeErr error
	for _, tg := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			qctx, cancel := context.WithTimeout(ctx, timeout)
			res, err := p.Query(qctx, tg.protocol, tg.addr)
			cancel()
			if err != nil {
				logger.V(1).Info("player query failed", "gameserver", tg.namespace+"/"+tg.name, "error", err.Error())
				return
			}
			err = p.Store.Set(ctx, tg.namespace, tg.name, panelcache.PlayersSnapshot{
				Online: res.Online, Max: res.Max, Players: res.Players, At: time.Now(),
			})
			if err != nil {
				mu.Lock()
				if storeErr == nil {
					storeErr = fmt.Errorf("storing players of %s/%s: %w", tg.namespace, tg.name, err)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	p.report(logger, storeErr)
}

func (p *PlayersSampler) targets(ctx context.Context) ([]playersTarget, error) {
	var tenants gameserversv1alpha1.TenantList
	if err := p.Client.List(ctx, &tenants); err != nil {
		return nil, fmt.Errorf("listing tenants: %w", err)
	}
	eggs := map[types.NamespacedName]*gameserversv1alpha1.Egg{} // nil = not found
	var out []playersTarget
	for _, tn := range tenants.Items {
		if tn.Status.Phase != gameserversv1alpha1.TenantPhaseActive || tn.Status.Namespace == "" {
			continue
		}
		var list gameserversv1alpha1.GameServerList
		if err := p.Client.List(ctx, &list, client.InNamespace(tn.Status.Namespace)); err != nil {
			return nil, fmt.Errorf("listing game servers in %s: %w", tn.Status.Namespace, err)
		}
		for i := range list.Items {
			gs := &list.Items[i]
			if gs.Status.Phase != gameserversv1alpha1.GameServerPhaseRunning {
				continue
			}
			key := types.NamespacedName{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}
			egg, seen := eggs[key]
			if !seen {
				var e gameserversv1alpha1.Egg
				switch err := p.Client.Get(ctx, key, &e); {
				case err == nil:
					egg = &e
				case apierrors.IsNotFound(err):
				default:
					return nil, fmt.Errorf("reading egg %s: %w", key, err)
				}
				eggs[key] = egg
			}
			if egg == nil {
				continue
			}
			port, ok := egg.Spec.QueryPort()
			if !ok {
				continue // no query, or one that predates the webhook and is invalid
			}
			out = append(out, playersTarget{
				namespace: gs.Namespace, name: gs.Name,
				protocol: string(egg.Spec.Query.Protocol),
				addr:     gameServerAddr(gs, port.ContainerPort),
			})
		}
	}
	return out, nil
}

func (p *PlayersSampler) report(logger interface {
	Error(error, string, ...any)
	Info(string, ...any)
}, err error) {
	switch {
	case err == nil && p.lastErr != "":
		logger.Info("player sampling recovered")
		p.lastErr = ""
	case err != nil && err.Error() != p.lastErr:
		logger.Error(err, "player sampling failed")
		p.lastErr = err.Error()
	}
}
