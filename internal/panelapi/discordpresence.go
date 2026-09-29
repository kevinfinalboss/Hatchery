package panelapi

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/discord"
)

// discordPresenceLease is the Lease that picks the one Panel replica holding the Gateway connection.
const discordPresenceLease = "hatchery-panel-discord"

// RunDiscordPresence keeps the bot online from exactly one replica (leader election on a Lease in
// the Panel's namespace). Commands do not depend on it: they arrive over HTTP.
func (s *Server) RunDiscordPresence(ctx context.Context, identity string) {
	if !s.discordEnabled() || s.PodNamespace == "" || s.Clientset == nil {
		return
	}
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: discordPresenceLease, Namespace: s.PodNamespace},
		Client:     s.Clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}
	presence := &discord.Presence{
		Client:   s.Bot.Client,
		Activity: s.presenceActivity,
		Interval: 5 * time.Minute,
		OnError:  func(err error) { discordLog.Error(err, "Discord Gateway (presence) connection failed") },
	}
	for ctx.Err() == nil {
		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   30 * time.Second,
			RenewDeadline:   20 * time.Second,
			RetryPeriod:     5 * time.Second,
			ReleaseOnCancel: true,
			Name:            discordPresenceLease,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: presence.Run,
				OnStoppedLeading: func() { discordLog.Info("stopped holding the Discord presence") },
			},
		})
	}
}

// presenceActivity is "Watching N servers": the running servers of every organization.
func (s *Server) presenceActivity(ctx context.Context) string {
	var tenants v1.TenantList
	if err := s.Client.List(ctx, &tenants); err != nil {
		return "Hatchery"
	}
	running := 0
	for _, tn := range tenants.Items {
		if tn.Status.Namespace == "" {
			continue
		}
		var list v1.GameServerList
		if s.Client.List(ctx, &list, client.InNamespace(tn.Status.Namespace)) != nil {
			continue
		}
		for _, gs := range list.Items {
			if gs.Status.Phase == v1.GameServerPhaseRunning {
				running++
			}
		}
	}
	if running == 1 {
		return "1 server"
	}
	return fmt.Sprintf("%d servers", running)
}
