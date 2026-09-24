/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// These are registered on controller-runtime's default registry, which the
// manager already serves at /metrics (see cmd/main.go and config/prometheus,
// where a ServiceMonitor is scaffolded to scrape it). They live alongside the
// generic controller_runtime_reconcile_* metrics controller-runtime emits for
// free; these three add the domain-specific view: how reconciles are going
// and what phase every GameServer is currently in.
var (
	gameServerReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hatchery_gameserver_reconcile_total",
			Help: "Total number of GameServer reconciles, partitioned by outcome.",
		},
		[]string{"result"},
	)

	gameServerReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "hatchery_gameserver_reconcile_duration_seconds",
			Help:    "Time spent per GameServer reconcile, in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"},
	)

	// gameServerPhase is 1 for a GameServer's current phase and 0 for phases it
	// previously occupied, so summing by phase across the namespace/name labels
	// gives an accurate "how many servers are in each phase right now" view
	// (the same pattern kube-state-metrics uses for kube_pod_status_phase).
	gameServerPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hatchery_gameserver_phase",
			Help: "Whether a GameServer is currently in a given phase (1) or not (0).",
		},
		[]string{"namespace", "name", "phase"},
	)

	scheduleRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "hatchery_schedule_runs_total",
		Help: "Finished GameServerSchedule runs by result (Succeeded, Failed, Skipped).",
	}, []string{"result"})
)

func init() {
	metrics.Registry.MustRegister(gameServerReconcileTotal, gameServerReconcileDuration, gameServerPhase, scheduleRunsTotal)
}

// recordGameServerPhase updates the phase gauge when a GameServer transitions
// from oldPhase to newPhase, clearing the series for the phase it left.
func recordGameServerPhase(gs *gameserversv1alpha1.GameServer, oldPhase, newPhase gameserversv1alpha1.GameServerPhase) {
	if oldPhase != "" && oldPhase != newPhase {
		gameServerPhase.WithLabelValues(gs.Namespace, gs.Name, string(oldPhase)).Set(0)
	}
	if newPhase != "" {
		gameServerPhase.WithLabelValues(gs.Namespace, gs.Name, string(newPhase)).Set(1)
	}
}

// removeGameServerMetrics drops every phase series for a deleted GameServer so
// it doesn't linger in the registry forever with a stale value. Called from
// the finalizer.
func removeGameServerMetrics(namespace, name string) {
	gameServerPhase.DeletePartialMatch(prometheus.Labels{"namespace": namespace, "name": name})
}
