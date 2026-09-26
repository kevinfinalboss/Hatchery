package controller

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// sinceLogs is a BackupLogReader that remembers the since it was asked for.
type sinceLogs struct {
	mu    sync.Mutex
	out   string
	since []time.Time
}

func (f *sinceLogs) LogsSince(_ context.Context, _, _, _ string, since time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	return f.out, nil
}

var _ = Describe("GameServerBackup world-save pause", func() {
	const ns = "default"
	ctx := context.Background()

	// setup creates an Egg with the given backup hooks, a GameServer, its PVC and — when running — a
	// Pod whose "server" container is running, and a backup of it. It returns the reconciler (with a
	// recording exec and a controllable clock) and the backup key.
	type env struct {
		r     *GameServerBackupReconciler
		key   types.NamespacedName
		execs *[]string
		clock *time.Time
		logs  *sinceLogs
		pod   *corev1.Pod
	}
	setup := func(name string, hooks *gameserversv1alpha1.EggBackup, running bool) env {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "img:1"}},
				StartCommand: "run",
				Backup:       hooks,
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
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, gs) })

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pvc) })

		var pod *corev1.Pod
		if running {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "server", Image: "img:1"}}},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })
			pod.Status.Phase = corev1.PodRunning
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "server",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}}}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
			gs.Status.PodName = pod.Name
			gs.Status.Phase = gameserversv1alpha1.GameServerPhaseRunning
			Expect(k8sClient.Status().Update(ctx, gs)).To(Succeed())
		}

		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: name},
				Destination: gameserversv1alpha1.BackupDestination{S3: &gameserversv1alpha1.S3Destination{
					Bucket: "b", SecretRef: corev1.LocalObjectReference{Name: "s3"}}},
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())

		execs := &[]string{}
		// Whole seconds: status times come back from the apiserver truncated to seconds, and the
		// exact RequeueAfter assertions below compare against them.
		clock := time.Now().Truncate(time.Second)
		logs := &sinceLogs{}
		r := &GameServerBackupReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(), Logs: logs,
			Now: func() time.Time { return clock },
			Exec: func(_ context.Context, _, _, container string, cmd []string) error {
				Expect(container).To(Equal("server"))
				*execs = append(*execs, cmd[len(cmd)-1])
				return nil
			},
		}
		e := env{r: r, key: types.NamespacedName{Name: name, Namespace: ns}, execs: execs, clock: &clock, logs: logs, pod: pod}
		DeferCleanup(func() {
			var cur gameserversv1alpha1.GameServerBackup
			if err := k8sClient.Get(ctx, e.key, &cur); err == nil {
				cur.Finalizers = nil
				_ = k8sClient.Update(ctx, &cur)
				_ = k8sClient.Delete(ctx, &cur)
			}
		})
		return e
	}
	reconcileOnce := func(e env) reconcile.Result {
		res, err := e.r.Reconcile(ctx, reconcile.Request{NamespacedName: e.key})
		Expect(err).NotTo(HaveOccurred())
		return res
	}
	getBackup := func(e env) *gameserversv1alpha1.GameServerBackup {
		var b gameserversv1alpha1.GameServerBackup
		Expect(k8sClient.Get(ctx, e.key, &b)).To(Succeed())
		return &b
	}
	finishJob := func(e env) {
		b := getBackup(e)
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: b.Status.JobName}, &job)).To(Succeed())
		job.Status.Succeeded = 1
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.CompletionTime = &now
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, LastTransitionTime: now},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: now},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}
	hooks := func() *gameserversv1alpha1.EggBackup {
		return &gameserversv1alpha1.EggBackup{Before: []string{"save-off", "save-all flush"}, After: []string{"save-on"}, SavedRegex: "Saved the game"}
	}

	It("pauses saving, waits for the regex, backs up, then resumes", func() {
		e := setup("quiesce-regex", hooks(), true)
		reconcileOnce(e) // finalizer
		res := reconcileOnce(e)
		Expect(*e.execs).To(Equal([]string{"save-off", "save-all flush"}))
		b := getBackup(e)
		Expect(b.Status.Quiesce).NotTo(BeNil())
		Expect(b.Status.Quiesce.PodUID).To(Equal(string(e.pod.UID)))
		Expect(b.Status.JobName).To(BeEmpty(), "no Job before the world is saved")
		Expect(res.RequeueAfter).To(Equal(3 * time.Second))

		By("a repeated reconcile before the save does not resend before")
		reconcileOnce(e)
		Expect(*e.execs).To(HaveLen(2))

		By("the console prints the save line")
		e.logs.out = "[Server thread/INFO]: Saved the game"
		reconcileOnce(e)
		b = getBackup(e)
		Expect(b.Status.JobName).NotTo(BeEmpty())
		c := apimeta.FindStatusCondition(b.Status.Conditions, gameserversv1alpha1.BackupConditionQuiesced)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
		Expect(c.Reason).To(Equal(gameserversv1alpha1.QuiesceReasonSaved))
		for _, since := range e.logs.since {
			Expect(since.Unix()).To(Equal(b.Status.Quiesce.BeforeSentAt.Unix()), "only the console after the commands counts")
		}

		By("the Job finishes and save-on goes to the same Pod")
		finishJob(e)
		reconcileOnce(e) // Completed
		reconcileOnce(e) // resume
		Expect(*e.execs).To(Equal([]string{"save-off", "save-all flush", "save-on"}))
		b = getBackup(e)
		Expect(b.Status.Quiesce.ResumedAt).NotTo(BeNil())
		c = apimeta.FindStatusCondition(b.Status.Conditions, gameserversv1alpha1.BackupConditionResumed)
		Expect(c.Reason).To(Equal(gameserversv1alpha1.ResumeReasonSent))

		By("resumed backups are left alone")
		reconcileOnce(e)
		Expect(*e.execs).To(HaveLen(3))
	})

	It("backs up anyway when the regex never matches", func() {
		e := setup("quiesce-timeout", hooks(), true)
		reconcileOnce(e)
		reconcileOnce(e)
		*e.clock = e.clock.Add(61 * time.Second)
		reconcileOnce(e)
		b := getBackup(e)
		Expect(b.Status.JobName).NotTo(BeEmpty())
		c := apimeta.FindStatusCondition(b.Status.Conditions, gameserversv1alpha1.BackupConditionQuiesced)
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Reason).To(Equal(gameserversv1alpha1.QuiesceReasonTimeout))
	})

	It("uses the fixed delay without a regex", func() {
		e := setup("quiesce-delay", &gameserversv1alpha1.EggBackup{Before: []string{"save"}}, true)
		reconcileOnce(e)
		res := reconcileOnce(e)
		Expect(res.RequeueAfter).To(Equal(10 * time.Second))
		*e.clock = e.clock.Add(10 * time.Second)
		reconcileOnce(e)
		c := apimeta.FindStatusCondition(getBackup(e).Status.Conditions, gameserversv1alpha1.BackupConditionQuiesced)
		Expect(c.Reason).To(Equal(gameserversv1alpha1.QuiesceReasonDelay))
		Expect(e.logs.since).To(BeEmpty(), "no regex, no log reads")
	})

	It("sends nothing for a stopped server", func() {
		e := setup("quiesce-stopped", hooks(), false)
		reconcileOnce(e)
		reconcileOnce(e)
		b := getBackup(e)
		Expect(*e.execs).To(BeEmpty())
		Expect(b.Status.JobName).NotTo(BeEmpty())
		Expect(b.Status.Quiesce).To(BeNil())
	})

	It("does not send save-on to a replaced Pod", func() {
		e := setup("quiesce-replaced", hooks(), true)
		reconcileOnce(e)
		reconcileOnce(e)
		e.logs.out = "Saved the game"
		reconcileOnce(e)
		Expect(k8sClient.Delete(ctx, e.pod)).To(Succeed()) // the server restarted mid-backup
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: e.pod.Name}, &corev1.Pod{})
		}).ShouldNot(Succeed())
		finishJob(e)
		reconcileOnce(e)
		reconcileOnce(e)
		Expect(*e.execs).To(HaveLen(2))
		c := apimeta.FindStatusCondition(getBackup(e).Status.Conditions, gameserversv1alpha1.BackupConditionResumed)
		Expect(c.Reason).To(Equal(gameserversv1alpha1.ResumeReasonPodReplaced))
	})

	It("sends save-on when the backup is deleted mid-way", func() {
		e := setup("quiesce-deleted", hooks(), true)
		reconcileOnce(e)
		reconcileOnce(e)
		Expect(k8sClient.Delete(ctx, getBackup(e))).To(Succeed())
		reconcileOnce(e)
		Expect(*e.execs).To(Equal([]string{"save-off", "save-all flush", "save-on"}))
	})

	It("gives up on save-on after five failures and says so", func() {
		e := setup("quiesce-giveup", hooks(), true)
		reconcileOnce(e)
		reconcileOnce(e)
		e.logs.out = "Saved the game"
		reconcileOnce(e)
		finishJob(e)
		reconcileOnce(e)
		e.r.Exec = func(context.Context, string, string, string, []string) error { return context.DeadlineExceeded }
		for i := 1; i < 5; i++ {
			res := reconcileOnce(e)
			Expect(res.RequeueAfter).To(Equal(time.Duration(i) * 10 * time.Second))
		}
		reconcileOnce(e)
		b := getBackup(e)
		Expect(b.Status.Quiesce.ResumedAt).NotTo(BeNil())
		c := apimeta.FindStatusCondition(b.Status.Conditions, gameserversv1alpha1.BackupConditionResumed)
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Reason).To(Equal(gameserversv1alpha1.QuiesceReasonSendFailed))
		Expect(b.Status.Phase).To(Equal(gameserversv1alpha1.GameServerBackupPhaseCompleted))
	})
})
