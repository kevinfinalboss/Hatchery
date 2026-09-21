package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

type fakeLogs struct{ out string }

func (f *fakeLogs) TailLogs(context.Context, string, string, string, int64) (string, error) {
	return f.out, nil
}

var _ = Describe("GameServer status phases", func() {
	const ns = "default"
	ctx := context.Background()

	setup := func(name string, detection *gameserversv1alpha1.EggStartupDetection) (*GameServerReconciler, *fakeLogs, types.NamespacedName, *gameserversv1alpha1.GameServer) {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images:           []gameserversv1alpha1.EggImage{{Name: "default", Image: "img:1"}},
				StartCommand:     "run",
				StartupDetection: detection,
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, egg) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: name},
				State:   gameserversv1alpha1.GameServerStateRunning,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		key := types.NamespacedName{Name: name, Namespace: ns}
		logs := &fakeLogs{}
		r := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage, LogReader: logs}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, gs)
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		})
		for range 2 { // finalizer, then resources
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
		return r, logs, key, gs
	}
	setPodStatus := func(key types.NamespacedName, mutate func(*corev1.PodStatus)) {
		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		mutate(&pod.Status)
		Expect(k8sClient.Status().Update(ctx, &pod)).To(Succeed())
	}
	reconcileOnce := func(r *GameServerReconciler, key types.NamespacedName) reconcile.Result {
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		return res
	}
	phaseOf := func(key types.NamespacedName, gs *gameserversv1alpha1.GameServer) gameserversv1alpha1.GameServerPhase {
		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		return gs.Status.Phase
	}
	readyOf := func(gs *gameserversv1alpha1.GameServer) *metav1.Condition {
		return apimeta.FindStatusCondition(gs.Status.Conditions, gameserversv1alpha1.ConditionReady)
	}
	serverRunning := func(startedAgo time.Duration) func(*corev1.PodStatus) {
		return func(s *corev1.PodStatus) {
			s.Phase = corev1.PodRunning
			s.ContainerStatuses = []corev1.ContainerStatus{{
				Name:  "server",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Now().Add(-startedAgo))}},
			}}
		}
	}

	It("goes Installing, Starting, Running as the install finishes and the game reports it is up", func() {
		r, logs, key, gs := setup("status-flow", &gameserversv1alpha1.EggStartupDetection{Regex: `Done \(`})

		By("the install init container is running")
		setPodStatus(key, func(s *corev1.PodStatus) {
			s.Phase = corev1.PodPending
			s.InitContainerStatuses = []corev1.ContainerStatus{{
				Name: "install", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}
		})
		reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseInstalling))

		By("the game process is up but has not printed the marker line yet")
		logs.out = "loading world…"
		setPodStatus(key, serverRunning(2*time.Second))
		res := reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseStarting))
		Expect(readyOf(gs).Status).To(Equal(metav1.ConditionFalse))
		Expect(res.RequeueAfter).To(BeNumerically(">", 0), "keeps polling the log while starting")

		By("the game prints the marker line")
		logs.out = "loading world…\nDone (3.1s)! For help, type help"
		res = reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseRunning))
		Expect(readyOf(gs).Status).To(Equal(metav1.ConditionTrue))
		Expect(res.RequeueAfter).To(BeZero(), "stops polling once it is up")

		By("stopping shows Stopping while the Pod winds down, then Stopped")
		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseStopping))
		reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseStopped))
	})

	It("treats a server whose startup line never appears as running once the timeout passes", func() {
		r, logs, key, gs := setup("status-timeout", &gameserversv1alpha1.EggStartupDetection{Regex: `never`, TimeoutSeconds: ptr.To(int32(5))})
		logs.out = "still booting"
		setPodStatus(key, serverRunning(30*time.Second))
		reconcileOnce(r, key)

		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseRunning))
		ready := readyOf(gs)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("StartupTimeout"))
	})

	It("goes straight to Running when the Egg has no startup detection", func() {
		r, _, key, gs := setup("status-nodetect", nil)
		setPodStatus(key, serverRunning(time.Second))
		reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseRunning))
		Expect(readyOf(gs).Status).To(Equal(metav1.ConditionTrue))
	})

	It("does not carry Ready over to a new Pod after a restart", func() {
		r, logs, key, gs := setup("status-restart", &gameserversv1alpha1.EggStartupDetection{Regex: `Done`})
		logs.out = "Done"
		setPodStatus(key, serverRunning(time.Minute))
		reconcileOnce(r, key)
		Expect(readyOf(gs).Status).To(Equal(metav1.ConditionTrue))

		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		gs.Annotations = map[string]string{gameserversv1alpha1.RestartAnnotation: "r1"}
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce(r, key) // deletes the Pod
		reconcileOnce(r, key) // creates the new one
		logs.out = "booting again"
		setPodStatus(key, serverRunning(time.Second))
		reconcileOnce(r, key)
		Expect(phaseOf(key, gs)).To(Equal(gameserversv1alpha1.GameServerPhaseStarting))
		Expect(readyOf(gs).Status).To(Equal(metav1.ConditionFalse))
	})
})
