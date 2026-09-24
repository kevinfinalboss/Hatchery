package controller

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

type failingLogs struct{}

func (failingLogs) TailLogs(context.Context, string, string, string, int64) (string, error) {
	return "", errors.New("kubelet unreachable")
}

var _ = Describe("GameServer crash recovery", func() {
	const ns = "default"
	ctx := context.Background()

	setup := func(name string, autoRestart *bool, detection *gameserversv1alpha1.EggStartupDetection, logs LogReader) (*GameServerReconciler, types.NamespacedName) {
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
				EggRef:      gameserversv1alpha1.GameServerEggRef{Name: name},
				State:       gameserversv1alpha1.GameServerStateRunning,
				AutoRestart: autoRestart,
				Storage:     gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		key := types.NamespacedName{Name: name, Namespace: ns}
		r := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage, LogReader: logs}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: gameserversv1alpha1.CrashLogConfigMapName(name), Namespace: ns}})
			_ = k8sClient.Delete(ctx, gs)
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		})
		for range 2 { // finalizer, then resources
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
		return r, key
	}
	reconcileOnce := func(r *GameServerReconciler, key types.NamespacedName) reconcile.Result {
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		return res
	}
	getGS := func(key types.NamespacedName) *gameserversv1alpha1.GameServer {
		var gs gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, key, &gs)).To(Succeed())
		return &gs
	}
	podUID := func(key types.NamespacedName) types.UID {
		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		return pod.UID
	}
	podGone := func(key types.NamespacedName) bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, key, &corev1.Pod{}))
	}
	// crash marks the game container as terminated while the sftp-agent sidecar keeps the Pod Running,
	// which is exactly what a real crash looks like with RestartPolicy Never.
	crash := func(key types.NamespacedName, exit int32, reason string, ago time.Duration) {
		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		pod.Status.Phase = corev1.PodRunning
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: "server", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: exit, Reason: reason, FinishedAt: metav1.NewTime(time.Now().Add(-ago)),
			}}},
			{Name: "sftp-agent", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}},
		}
		Expect(k8sClient.Status().Update(ctx, &pod)).To(Succeed())
	}
	crashLog := func(key types.NamespacedName) (*corev1.ConfigMap, error) {
		var cm corev1.ConfigMap
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: gameserversv1alpha1.CrashLogConfigMapName(key.Name)}, &cm)
		return &cm, err
	}
	crashedCond := func(gs *gameserversv1alpha1.GameServer) *metav1.Condition {
		return apimeta.FindStatusCondition(gs.Status.Conditions, gameserversv1alpha1.ConditionCrashed)
	}

	It("waits before restarting, shows Crashed, and saves the console once", func() {
		logs := &fakeLogs{out: "Exception in server tick loop"}
		r, key := setup("crash-wait", nil, nil, logs)
		uid := podUID(key)

		crash(key, 1, "Error", 0)
		res := reconcileOnce(r, key)
		gs := getGS(key)
		Expect(gs.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseCrashed))
		Expect(gs.Status.RecentCrashes).To(HaveLen(1))
		Expect(gs.Status.LastCrash).NotTo(BeNil())
		Expect(gs.Status.LastCrash.ExitCode).To(Equal(int32(1)))
		Expect(res.RequeueAfter).To(And(BeNumerically(">", 0), BeNumerically("<=", 10*time.Second)))
		Expect(podUID(key)).To(Equal(uid), "the Pod is kept until the wait is over")

		cm, err := crashLog(key)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data).To(HaveKeyWithValue(gameserversv1alpha1.CrashLogKeyLog, "Exception in server tick loop"))
		Expect(cm.Data).To(HaveKeyWithValue(gameserversv1alpha1.CrashLogKeyExitCode, "1"))
		Expect(cm.OwnerReferences).To(HaveLen(1))

		By("reconciling the same crash again neither counts it twice nor rereads the console")
		logs.out = "something else"
		reconcileOnce(r, key)
		Expect(getGS(key).Status.RecentCrashes).To(HaveLen(1))
		cm, _ = crashLog(key)
		Expect(cm.Data[gameserversv1alpha1.CrashLogKeyLog]).To(Equal("Exception in server tick loop"))
	})

	It("restarts twice, then gives up on the third crash in the window and can be started again", func() {
		logs := &fakeLogs{out: "first"}
		r, key := setup("crash-loop", nil, nil, logs)

		By("a crash whose wait already passed restarts right away")
		first := podUID(key)
		crash(key, 1, "Error", 2*time.Minute)
		reconcileOnce(r, key)
		Expect(podGone(key)).To(BeTrue())
		reconcileOnce(r, key)
		second := podUID(key)
		Expect(second).NotTo(Equal(first))
		Expect(getGS(key).Status.Phase).NotTo(Equal(gameserversv1alpha1.GameServerPhaseCrashed))

		By("the second crash overwrites the saved console")
		logs.out = "second"
		crash(key, 1, "Error", time.Minute)
		reconcileOnce(r, key)
		Expect(podGone(key)).To(BeTrue())
		cm, _ := crashLog(key)
		Expect(cm.Data[gameserversv1alpha1.CrashLogKeyLog]).To(Equal("second"))
		reconcileOnce(r, key)

		By("the third crash in 10 minutes stops the server")
		crash(key, 137, "OOMKilled", time.Second)
		reconcileOnce(r, key)
		gs := getGS(key)
		Expect(gs.Spec.State).To(Equal(gameserversv1alpha1.GameServerStateStopped))
		Expect(crashedCond(gs)).NotTo(BeNil())
		Expect(crashedCond(gs).Status).To(Equal(metav1.ConditionTrue))
		Expect(crashedCond(gs).Reason).To(Equal("CrashLoop"))
		Expect(gs.Status.LastCrash.OOMKilled).To(BeTrue())
		Expect(podGone(key)).To(BeTrue())

		reconcileOnce(r, key)
		gs = getGS(key)
		Expect(gs.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseStopped))
		Expect(gs.Status.RecentCrashes).To(BeEmpty(), "a stopped server starts a fresh count")
		Expect(gs.Status.LastCrash).NotTo(BeNil(), "the last crash stays visible")
		Expect(crashedCond(gs).Status).To(Equal(metav1.ConditionTrue), "still says why it is stopped")

		By("starting it again clears the Crashed condition")
		gs.Spec.State = gameserversv1alpha1.GameServerStateRunning
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce(r, key)
		gs = getGS(key)
		Expect(crashedCond(gs).Status).To(Equal(metav1.ConditionFalse))
		Expect(podGone(key)).To(BeFalse())
	})

	It("stops the server without recording a crash when the game exits with code 0", func() {
		r, key := setup("crash-clean", nil, nil, &fakeLogs{out: "bye"})
		crash(key, 0, "Completed", 0)
		reconcileOnce(r, key)
		gs := getGS(key)
		Expect(gs.Spec.State).To(Equal(gameserversv1alpha1.GameServerStateStopped))
		Expect(gs.Status.LastCrash).To(BeNil())
		Expect(crashedCond(gs)).To(Or(BeNil(), HaveField("Status", metav1.ConditionFalse)))
		Expect(podGone(key)).To(BeTrue())
		_, err := crashLog(key)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a clean exit saves no crash log")
	})

	It("gives up on the first crash when autoRestart is off", func() {
		r, key := setup("crash-off", ptr.To(false), nil, &fakeLogs{out: "java.lang.OutOfMemoryError"})
		crash(key, 137, "OOMKilled", 0)
		reconcileOnce(r, key)
		gs := getGS(key)
		Expect(gs.Spec.State).To(Equal(gameserversv1alpha1.GameServerStateStopped))
		Expect(crashedCond(gs).Reason).To(Equal("AutoRestartDisabled"))
		cm, err := crashLog(key)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data).To(HaveKeyWithValue(gameserversv1alpha1.CrashLogKeyOOMKilled, "true"))
	})

	It("still restarts when the crashed console cannot be read", func() {
		r, key := setup("crash-nolog", nil, nil, failingLogs{})
		crash(key, 1, "Error", time.Minute)
		reconcileOnce(r, key)
		Expect(podGone(key)).To(BeTrue())
		cm, err := crashLog(key)
		Expect(err).NotTo(HaveOccurred())
		Expect(cm.Data[gameserversv1alpha1.CrashLogKeyLog]).To(BeEmpty())
	})

	It("does not restart a crashed server the user stops during the wait", func() {
		r, key := setup("crash-stop", nil, nil, &fakeLogs{})
		crash(key, 1, "Error", 0)
		reconcileOnce(r, key)
		gs := getGS(key)
		Expect(gs.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseCrashed))

		gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce(r, key)
		reconcileOnce(r, key)
		gs = getGS(key)
		Expect(gs.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseStopped))
		Expect(gs.Status.RecentCrashes).To(BeEmpty())
		Expect(podGone(key)).To(BeTrue())
	})

	It("serves a restart request during the wait as a restart, not as crash handling", func() {
		r, key := setup("crash-restart", nil, nil, &fakeLogs{})
		crash(key, 1, "Error", 0)
		reconcileOnce(r, key)

		gs := getGS(key)
		gs.Annotations = map[string]string{gameserversv1alpha1.RestartAnnotation: "now"}
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce(r, key)
		Expect(podGone(key)).To(BeTrue())
		reconcileOnce(r, key)
		Expect(podGone(key)).To(BeFalse())
		Expect(getGS(key).Status.Phase).NotTo(Equal(gameserversv1alpha1.GameServerPhaseCrashed))
	})

	It("does not leave a crashed server in Starting when the Egg has startup detection", func() {
		r, key := setup("crash-detect", nil, &gameserversv1alpha1.EggStartupDetection{Regex: `Done`}, &fakeLogs{})
		crash(key, 1, "Error", 0)
		reconcileOnce(r, key)
		Expect(getGS(key).Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseCrashed))
	})
})
