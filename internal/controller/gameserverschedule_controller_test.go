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
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/backupplan"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// runningServer creates a GameServer (and the Egg it references) in "default" and marks it
// Running with a pod name, the state a schedule acts on.
func runningServer(ctx context.Context, name string) *gameserversv1alpha1.GameServer {
	return runningServerIn(ctx, "default", name)
}

func runningServerIn(ctx context.Context, ns, name string) *gameserversv1alpha1.GameServer {
	egg := &gameserversv1alpha1.Egg{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-egg", Namespace: ns},
		Spec: gameserversv1alpha1.EggSpec{
			Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:1"}},
			StartCommand: "run",
		},
	}
	Expect(k8sClient.Create(ctx, egg)).To(Succeed())
	gs := &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: egg.Name},
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			State:   gameserversv1alpha1.GameServerStateRunning,
		},
	}
	Expect(k8sClient.Create(ctx, gs)).To(Succeed())
	gs.Status.Phase = gameserversv1alpha1.GameServerPhaseRunning
	gs.Status.PodName = name
	Expect(k8sClient.Status().Update(ctx, gs)).To(Succeed())
	return gs
}

// newSchedule builds (does not create) a schedule for server in "default".
func newSchedule(name, server, cron string, tasks ...gameserversv1alpha1.ScheduleTask) *gameserversv1alpha1.GameServerSchedule {
	return &gameserversv1alpha1.GameServerSchedule{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: gameserversv1alpha1.GameServerScheduleSpec{
			GameServerRef: gameserversv1alpha1.GameServerRef{Name: server},
			Cron:          cron, OnlyWhenRunning: true, Tasks: tasks,
		},
	}
}

// reconcilerAt returns a schedule reconciler whose clock is clk and whose Exec records commands.
func reconcilerAt(clk *fakeClock, execs *[][]string) *GameServerScheduleReconciler {
	return &GameServerScheduleReconciler{
		Client: k8sClient, Scheme: k8sClient.Scheme(), Now: clk.Now,
		Exec: func(_ context.Context, _, _, container string, cmd []string) error {
			Expect(container).To(Equal("server"))
			*execs = append(*execs, cmd)
			return nil
		},
	}
}

var _ = Describe("GameServerSchedule", func() {
	ctx := context.Background()

	// The API server stamps CreationTimestamp with the real clock, and a schedule's first fire time
	// is counted from it. The fake clock therefore runs on the next (real) UTC day, so "03:59"
	// always comes after the schedule's creation.
	var day time.Time
	BeforeEach(func() {
		day = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	})
	at := func(hh, mm, ss int) time.Time {
		return day.Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute + time.Duration(ss)*time.Second)
	}

	cmd := func(c string, delay int32) gameserversv1alpha1.ScheduleTask {
		return gameserversv1alpha1.ScheduleTask{Action: gameserversv1alpha1.ScheduleActionCommand, Command: c, DelaySeconds: delay}
	}
	action := func(a gameserversv1alpha1.ScheduleAction) gameserversv1alpha1.ScheduleTask {
		return gameserversv1alpha1.ScheduleTask{Action: a}
	}
	consoleCmd := func(c string) []string {
		return []string{"sh", "-c", "printf '%s\\n' \"$1\" > /proc/1/fd/0", "sh", c}
	}
	reconcileSched := func(r *GameServerScheduleReconciler, name string) reconcile.Result {
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: name}})
		Expect(err).NotTo(HaveOccurred())
		return res
	}
	getSched := func(name string) *gameserversv1alpha1.GameServerSchedule {
		var s gameserversv1alpha1.GameServerSchedule
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, &s)).To(Succeed())
		return &s
	}
	getServer := func(ns, name string) *gameserversv1alpha1.GameServer {
		var gs gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &gs)).To(Succeed())
		return &gs
	}
	setPhase := func(gs *gameserversv1alpha1.GameServer, phase gameserversv1alpha1.GameServerPhase) {
		gs.Status.Phase = phase
		Expect(k8sClient.Status().Update(ctx, gs)).To(Succeed())
	}
	// fireAt04 reconciles the schedule at 03:59 (nothing due) and again at 04:00:05 (the fire time).
	fireAt04 := func(clk *fakeClock, r *GameServerScheduleReconciler, name string) reconcile.Result {
		clk.t = at(3, 59, 0)
		reconcileSched(r, name)
		clk.t = at(4, 0, 5)
		return reconcileSched(r, name)
	}

	Context("when to fire", func() {
		It("does not fire early and publishes nextScheduleTime", func() {
			runningServer(ctx, "sch-early")
			Expect(k8sClient.Create(ctx, newSchedule("sch-early", "sch-early", "0 4 * * *", cmd("say hi", 0)))).To(Succeed())
			clk := &fakeClock{t: at(3, 59, 0)}
			var execs [][]string
			res := reconcileSched(reconcilerAt(clk, &execs), "sch-early")

			s := getSched("sch-early")
			Expect(s.Status.NextScheduleTime).NotTo(BeNil())
			Expect(s.Status.NextScheduleTime.Time).To(BeTemporally("==", at(4, 0, 0)))
			Expect(s.Status.LastRun).To(BeNil())
			Expect(res.RequeueAfter).To(BeNumerically("~", time.Minute, time.Second))
			Expect(execs).To(BeEmpty())
			Expect(apimeta.IsStatusConditionTrue(s.Status.Conditions, "Ready")).To(BeTrue())
			Expect(metav1.IsControlledBy(s, getServer("default", "sch-early"))).To(BeTrue())
		})

		It("fires on time and runs the tasks", func() {
			runningServer(ctx, "sch-fire")
			Expect(k8sClient.Create(ctx, newSchedule("sch-fire", "sch-fire", "0 4 * * *",
				cmd("say hi", 0), action(gameserversv1alpha1.ScheduleActionRestart)))).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			fireAt04(clk, reconcilerAt(clk, &execs), "sch-fire")

			Expect(execs).To(Equal([][]string{consoleCmd("say hi")}))
			gs := getServer("default", "sch-fire")
			Expect(gs.Annotations).To(HaveKey(gameserversv1alpha1.RestartAnnotation))
			s := getSched("sch-fire")
			Expect(s.Status.LastRun).NotTo(BeNil())
			Expect(s.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSucceeded))
			Expect(s.Status.ActiveRun).To(BeNil())
			Expect(s.Status.LastScheduleTime.Time).To(BeTemporally("==", at(4, 0, 0)))
		})

		It("waits between tasks and resumes after an operator restart", func() {
			runningServer(ctx, "sch-wait")
			Expect(k8sClient.Create(ctx, newSchedule("sch-wait", "sch-wait", "0 4 * * *", cmd("a", 0), cmd("b", 60)))).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			res := fireAt04(clk, reconcilerAt(clk, &execs), "sch-wait")

			Expect(execs).To(Equal([][]string{consoleCmd("a")}))
			s := getSched("sch-wait")
			Expect(s.Status.ActiveRun).NotTo(BeNil())
			Expect(s.Status.ActiveRun.TaskIndex).To(Equal(int32(1)))
			Expect(res.RequeueAfter).To(BeNumerically("~", 60*time.Second, time.Second))

			// A brand-new reconciler: nothing but the status carries the run over.
			var execs2 [][]string
			clk2 := &fakeClock{t: clk.t}
			clk2.Advance(61 * time.Second)
			reconcileSched(reconcilerAt(clk2, &execs2), "sch-wait")
			Expect(execs2).To(Equal([][]string{consoleCmd("b")}))
			s = getSched("sch-wait")
			Expect(s.Status.ActiveRun).To(BeNil())
			Expect(s.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSucceeded))
		})

		It("skips a fire time missed beyond the window", func() {
			runningServer(ctx, "sch-missed")
			Expect(k8sClient.Create(ctx, newSchedule("sch-missed", "sch-missed", "0 4 * * *", cmd("say hi", 0)))).To(Succeed())
			s := getSched("sch-missed")
			s.Status.LastScheduleTime = &metav1.Time{Time: at(4, 0, 0).Add(-24 * time.Hour)}
			Expect(k8sClient.Status().Update(ctx, s)).To(Succeed())

			clk := &fakeClock{t: at(10, 0, 0)}
			var execs [][]string
			reconcileSched(reconcilerAt(clk, &execs), "sch-missed")

			s = getSched("sch-missed")
			Expect(s.Status.LastRun).NotTo(BeNil())
			Expect(s.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSkipped))
			Expect(s.Status.LastRun.Message).To(ContainSubstring("missed"))
			Expect(execs).To(BeEmpty())
			Expect(s.Status.NextScheduleTime.Time).To(BeTemporally("==", at(4, 0, 0).Add(24*time.Hour)))
		})

		It("skips the run while the server is stopped and onlyWhenRunning is set", func() {
			gs := runningServer(ctx, "sch-stopped")
			setPhase(gs, gameserversv1alpha1.GameServerPhaseStopped)
			Expect(k8sClient.Create(ctx, newSchedule("sch-stopped", "sch-stopped", "0 4 * * *", cmd("say hi", 0)))).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			fireAt04(clk, reconcilerAt(clk, &execs), "sch-stopped")

			s := getSched("sch-stopped")
			Expect(s.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSkipped))
			Expect(s.Status.LastRun.Message).To(Equal("server is not running"))
			Expect(execs).To(BeEmpty())
		})

		It("skips the run on a suspended server even without onlyWhenRunning", func() {
			gs := runningServer(ctx, "sch-susp")
			gs.Spec.Suspended = true
			Expect(k8sClient.Update(ctx, gs)).To(Succeed())
			sched := newSchedule("sch-susp", "sch-susp", "0 4 * * *", cmd("say hi", 0))
			sched.Spec.OnlyWhenRunning = false
			Expect(k8sClient.Create(ctx, sched)).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			fireAt04(clk, reconcilerAt(clk, &execs), "sch-susp")

			s := getSched("sch-susp")
			Expect(s.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSkipped))
			Expect(s.Status.LastRun.Message).To(ContainSubstring("suspended"))
			Expect(execs).To(BeEmpty())
		})

		It("runs once per distinct run-now value", func() {
			runningServer(ctx, "sch-now")
			sched := newSchedule("sch-now", "sch-now", "0 4 * * *", cmd("say hi", 0))
			sched.Annotations = map[string]string{gameserversv1alpha1.RunNowAnnotation: "1"}
			Expect(k8sClient.Create(ctx, sched)).To(Succeed())
			clk := &fakeClock{t: at(12, 0, 0)}
			var execs [][]string
			r := reconcilerAt(clk, &execs)

			reconcileSched(r, "sch-now")
			Expect(execs).To(HaveLen(1))
			Expect(getSched("sch-now").Status.LastRunNow).To(Equal("1"))

			clk.Advance(time.Minute)
			reconcileSched(r, "sch-now")
			Expect(execs).To(HaveLen(1))

			s := getSched("sch-now")
			s.Annotations[gameserversv1alpha1.RunNowAnnotation] = "2"
			Expect(k8sClient.Update(ctx, s)).To(Succeed())
			clk.Advance(time.Minute)
			reconcileSched(r, "sch-now")
			Expect(execs).To(HaveLen(2))
			Expect(getSched("sch-now").Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSucceeded))
		})

		It("reports a missing GameServer and keeps checking", func() {
			Expect(k8sClient.Create(ctx, newSchedule("sch-ghost", "ghost", "0 4 * * *", cmd("say hi", 0)))).To(Succeed())
			clk := &fakeClock{t: at(3, 59, 0)}
			var execs [][]string
			res := reconcileSched(reconcilerAt(clk, &execs), "sch-ghost")

			cond := apimeta.FindStatusCondition(getSched("sch-ghost").Status.Conditions, "Ready")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("GameServerMissing"))
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		})

		It("reports an invalid spec and waits for an edit", func() {
			runningServer(ctx, "sch-invalid")
			sched := newSchedule("sch-invalid", "sch-invalid", "0 4 * * *", cmd("say hi", 0))
			sched.Spec.TimeZone = "Mars/Olympus"
			Expect(k8sClient.Create(ctx, sched)).To(Succeed())
			clk := &fakeClock{t: at(3, 59, 0)}
			var execs [][]string
			res := reconcileSched(reconcilerAt(clk, &execs), "sch-invalid")

			cond := apimeta.FindStatusCondition(getSched("sch-invalid").Status.Conditions, "Ready")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("InvalidSpec"))
			Expect(res.RequeueAfter).To(BeZero())
		})
	})

	Context("tasks", func() {
		It("stops a running server, and fails to start a suspended one", func() {
			runningServer(ctx, "sch-stop")
			Expect(k8sClient.Create(ctx, newSchedule("sch-stop", "sch-stop", "0 4 * * *", action(gameserversv1alpha1.ScheduleActionStop)))).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			fireAt04(clk, reconcilerAt(clk, &execs), "sch-stop")
			Expect(getServer("default", "sch-stop").Spec.State).To(Equal(gameserversv1alpha1.GameServerStateStopped))
			Expect(getSched("sch-stop").Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSucceeded))

			// Start on a suspended server: run it directly (a run would be skipped before any task).
			gs := runningServer(ctx, "sch-start")
			gs.Spec.Suspended = true
			Expect(k8sClient.Update(ctx, gs)).To(Succeed())
			sched := newSchedule("sch-start", "sch-start", "0 4 * * *", action(gameserversv1alpha1.ScheduleActionStart))
			sched.Status.ActiveRun = &gameserversv1alpha1.ActiveScheduleRun{ID: "r", StartedAt: metav1.Time{Time: at(4, 0, 0)}, NextTaskAt: metav1.Time{Time: at(4, 0, 0)}}
			clk.t = at(4, 0, 5)
			reconcilerAt(clk, &execs).advance(ctx, sched, gs, clk.t)
			Expect(sched.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunFailed))
			Expect(sched.Status.LastRun.Message).To(ContainSubstring("suspended"))
		})

		It("skips a restart on a stopped server without failing the run", func() {
			gs := runningServer(ctx, "sch-rst-stopped")
			setPhase(gs, gameserversv1alpha1.GameServerPhaseStopped)
			sched := newSchedule("sch-rst-stopped", "sch-rst-stopped", "0 4 * * *", action(gameserversv1alpha1.ScheduleActionRestart))
			sched.Spec.OnlyWhenRunning = false
			Expect(k8sClient.Create(ctx, sched)).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			fireAt04(clk, reconcilerAt(clk, &execs), "sch-rst-stopped")

			Expect(getServer("default", "sch-rst-stopped").Annotations).NotTo(HaveKey(gameserversv1alpha1.RestartAnnotation))
			Expect(getSched("sch-rst-stopped").Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunSucceeded))
		})

		It("fails a command on a stopped server", func() {
			gs := runningServer(ctx, "sch-cmd-stopped")
			setPhase(gs, gameserversv1alpha1.GameServerPhaseStopped)
			sched := newSchedule("sch-cmd-stopped", "sch-cmd-stopped", "0 4 * * *", cmd("say hi", 0))
			sched.Spec.OnlyWhenRunning = false
			Expect(k8sClient.Create(ctx, sched)).To(Succeed())
			clk := &fakeClock{}
			var execs [][]string
			fireAt04(clk, reconcilerAt(clk, &execs), "sch-cmd-stopped")

			s := getSched("sch-cmd-stopped")
			Expect(s.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunFailed))
			Expect(s.Status.LastRun.Message).To(ContainSubstring("not running"))
			Expect(execs).To(BeEmpty())
		})

		It("makes one backup per run even when the task is retried", func() {
			gs := runningServer(ctx, "sch-bkp")
			gs.Spec.BackupTarget = &gameserversv1alpha1.BackupTarget{Connection: "x", Bucket: "b"}
			Expect(k8sClient.Update(ctx, gs)).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: backupplan.ConnectionSecretPrefix + "x", Namespace: "default",
					Labels: map[string]string{backupplan.ConnectionLabel: "x"}},
				Data: map[string][]byte{"endpoint": []byte("s3.example"), "buckets": []byte(`["b"]`)},
			})).To(Succeed())

			clk := &fakeClock{t: at(4, 0, 0)}
			var execs [][]string
			r := reconcilerAt(clk, &execs)
			r.Backups = &backupplan.Planner{Client: k8sClient, Now: clk.Now}
			sched := newSchedule("sch-bkp", "sch-bkp", "0 4 * * *", gameserversv1alpha1.ScheduleTask{Action: gameserversv1alpha1.ScheduleActionBackup})
			task := sched.Spec.Tasks[0]
			Expect(r.runTask(ctx, sched, gs, task, "run-1")).To(Succeed())
			clk.Advance(2 * time.Second) // a different backup name, so only the run ID dedupes
			Expect(r.runTask(ctx, sched, gs, task, "run-1")).To(Succeed())

			var list gameserversv1alpha1.GameServerBackupList
			Expect(k8sClient.List(ctx, &list, client.InNamespace("default"),
				client.MatchingLabels{gameserversv1alpha1.ScheduleLabel: "sch-bkp"})).To(Succeed())
			Expect(list.Items).To(HaveLen(1))
			Expect(list.Items[0].Annotations).To(HaveKeyWithValue(gameserversv1alpha1.ScheduleRunAnnotation, "run-1"))
			Expect(list.Items[0].Labels).To(HaveKeyWithValue(gameserversv1alpha1.BackupGameServerLabel, "sch-bkp"))
		})

		It("fails the run at the platform's backup limit and deletes nothing", func() {
			const ns, org = "sch-plat", "schplat"
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns,
				Labels: map[string]string{gameserversv1alpha1.LabelTenant: org}}})).To(Succeed())
			Expect(k8sClient.Create(ctx, &gameserversv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: org},
				Spec: gameserversv1alpha1.TenantSpec{Quota: gameserversv1alpha1.TenantQuota{
					CPU: resource.MustParse("1"), Memory: resource.MustParse("1Gi"), Storage: resource.MustParse("1Gi"),
					Backups: &gameserversv1alpha1.TenantBackupQuota{MaxPerServer: 1, MaxPerOrg: 5, RetentionDays: 7},
				}},
			})).To(Succeed())
			gs := runningServerIn(ctx, ns, "sch-plat")
			gs.Spec.BackupTarget = &gameserversv1alpha1.BackupTarget{Connection: backupplan.PlatformConnection}
			Expect(k8sClient.Update(ctx, gs)).To(Succeed())
			old := &gameserversv1alpha1.GameServerBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "sch-plat-old", Namespace: ns, Labels: map[string]string{
					gameserversv1alpha1.BackupGameServerLabel:  "sch-plat",
					gameserversv1alpha1.BackupDestinationLabel: backupplan.PlatformConnection,
				}},
				Spec: gameserversv1alpha1.GameServerBackupSpec{
					GameServerRef: gameserversv1alpha1.GameServerRef{Name: "sch-plat"},
					Destination: gameserversv1alpha1.BackupDestination{S3: &gameserversv1alpha1.S3Destination{
						Bucket: "plat", SecretRef: corev1.LocalObjectReference{Name: backupplan.PlatformSecret}}},
				},
			}
			Expect(k8sClient.Create(ctx, old)).To(Succeed())

			sched := &gameserversv1alpha1.GameServerSchedule{
				ObjectMeta: metav1.ObjectMeta{Name: "sch-plat", Namespace: ns,
					Annotations: map[string]string{gameserversv1alpha1.RunNowAnnotation: "1"}},
				Spec: gameserversv1alpha1.GameServerScheduleSpec{
					GameServerRef: gameserversv1alpha1.GameServerRef{Name: "sch-plat"},
					Cron:          "0 4 * * *", OnlyWhenRunning: true,
					Tasks: []gameserversv1alpha1.ScheduleTask{{Action: gameserversv1alpha1.ScheduleActionBackup, KeepLast: 1}},
				},
			}
			Expect(k8sClient.Create(ctx, sched)).To(Succeed())
			clk := &fakeClock{t: at(12, 0, 0)}
			var execs [][]string
			r := reconcilerAt(clk, &execs)
			r.Backups = &backupplan.Planner{Client: k8sClient, Now: clk.Now, Platform: backupplan.Platform{
				Endpoint: "s3.example", Bucket: "plat", SecretNamespace: "default", SecretName: "sch-plat-src"}}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "sch-plat"}})
			Expect(err).NotTo(HaveOccurred())

			var got gameserversv1alpha1.GameServerSchedule
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "sch-plat"}, &got)).To(Succeed())
			Expect(got.Status.LastRun.Result).To(Equal(gameserversv1alpha1.ScheduleRunFailed))
			Expect(got.Status.LastRun.Message).To(ContainSubstring("backup limit reached"))
			var list gameserversv1alpha1.GameServerBackupList
			Expect(k8sClient.List(ctx, &list, client.InNamespace(ns))).To(Succeed())
			Expect(list.Items).To(HaveLen(1))
			Expect(list.Items[0].Name).To(Equal("sch-plat-old"))
		})
	})

	Context("backup rotation", func() {
		// makeBackup creates a backup of the schedule (or a manual one when sched is "") in phase.
		makeBackup := func(name, sched string, phase gameserversv1alpha1.GameServerBackupPhase) {
			labels := map[string]string{gameserversv1alpha1.BackupGameServerLabel: "rot"}
			if sched != "" {
				labels[gameserversv1alpha1.ScheduleLabel] = sched
			}
			b := &gameserversv1alpha1.GameServerBackup{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
				Spec: gameserversv1alpha1.GameServerBackupSpec{
					GameServerRef: gameserversv1alpha1.GameServerRef{Name: "rot"},
					Destination: gameserversv1alpha1.BackupDestination{S3: &gameserversv1alpha1.S3Destination{
						Bucket: "b", SecretRef: corev1.LocalObjectReference{Name: "s"}}},
				},
			}
			Expect(k8sClient.Create(ctx, b)).To(Succeed())
			b.Status.Phase = phase
			Expect(k8sClient.Status().Update(ctx, b)).To(Succeed())
		}
		backupNames := func(prefix string) []string {
			var list gameserversv1alpha1.GameServerBackupList
			Expect(k8sClient.List(ctx, &list, client.InNamespace("default"))).To(Succeed())
			var names []string
			for _, b := range list.Items {
				if len(b.Name) >= len(prefix) && b.Name[:len(prefix)] == prefix {
					names = append(names, b.Name)
				}
			}
			return names
		}
		rotSchedule := func(name string, keep int32) *gameserversv1alpha1.GameServerSchedule {
			return newSchedule(name, "rot", "0 4 * * *",
				gameserversv1alpha1.ScheduleTask{Action: gameserversv1alpha1.ScheduleActionBackup, KeepLast: keep})
		}

		It("keeps the newest completed backups of the schedule only", func() {
			// Created in name order: the name breaks CreationTimestamp ties (second resolution).
			makeBackup("rota-0-failed", "rota", gameserversv1alpha1.GameServerBackupPhaseFailed)
			makeBackup("rota-1", "rota", gameserversv1alpha1.GameServerBackupPhaseCompleted)
			makeBackup("rota-2", "rota", gameserversv1alpha1.GameServerBackupPhaseCompleted)
			makeBackup("rota-3", "rota", gameserversv1alpha1.GameServerBackupPhaseCompleted)
			makeBackup("rota-manual", "", gameserversv1alpha1.GameServerBackupPhaseCompleted)

			clk := &fakeClock{t: at(4, 0, 0)}
			var execs [][]string
			Expect(reconcilerAt(clk, &execs).rotateBackups(ctx, rotSchedule("rota", 2))).To(Succeed())
			Expect(backupNames("rota-")).To(ConsistOf("rota-0-failed", "rota-2", "rota-3", "rota-manual"))
		})

		It("deletes nothing while the schedule's newest backup is not Completed", func() {
			makeBackup("rotb-1", "rotb", gameserversv1alpha1.GameServerBackupPhaseCompleted)
			makeBackup("rotb-2", "rotb", gameserversv1alpha1.GameServerBackupPhaseCompleted)
			makeBackup("rotb-3", "rotb", gameserversv1alpha1.GameServerBackupPhaseCompleted)
			makeBackup("rotb-4", "rotb", gameserversv1alpha1.GameServerBackupPhaseRunning)

			clk := &fakeClock{t: at(4, 0, 0)}
			var execs [][]string
			Expect(reconcilerAt(clk, &execs).rotateBackups(ctx, rotSchedule("rotb", 2))).To(Succeed())
			Expect(backupNames("rotb-")).To(ConsistOf("rotb-1", "rotb-2", "rotb-3", "rotb-4"))
		})
	})
})
