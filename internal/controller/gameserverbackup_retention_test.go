package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

var _ = Describe("GameServerBackup retention", func() {
	const namespace = "default"
	ctx := context.Background()

	newCompleted := func(name, expiresAt string) (*GameServerBackupReconciler, types.NamespacedName, *gameserversv1alpha1.GameServerBackup) {
		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: "whatever"},
				Destination: gameserversv1alpha1.BackupDestination{S3: &gameserversv1alpha1.S3Destination{
					Bucket: "b", SecretRef: corev1.LocalObjectReference{Name: "s3-creds"},
				}},
			},
		}
		if expiresAt != "" {
			bkp.Annotations = map[string]string{gameserversv1alpha1.BackupExpiresAtAnnotation: expiresAt}
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())
		key := types.NamespacedName{Name: name, Namespace: namespace}
		r := &GameServerBackupReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		DeferCleanup(func() {
			var cur gameserversv1alpha1.GameServerBackup
			if err := k8sClient.Get(ctx, key, &cur); err == nil {
				cur.Finalizers = nil
				_ = k8sClient.Update(ctx, &cur)
				_ = k8sClient.Delete(ctx, &cur)
			}
		})
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key}) // attaches the finalizer
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		bkp.Status.Phase = gameserversv1alpha1.GameServerBackupPhaseCompleted
		Expect(k8sClient.Status().Update(ctx, bkp)).To(Succeed())
		return r, key, bkp
	}

	It("deletes a completed backup whose expiry has passed", func() {
		r, key, bkp := newCompleted("expired-backup", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		Expect(bkp.DeletionTimestamp.IsZero()).To(BeFalse(), "the finalizer keeps it around until the snapshot is pruned")
	})

	It("keeps a backup that has not expired and asks to be looked at again at its expiry", func() {
		r, key, bkp := newCompleted("fresh-backup", time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339))
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		Expect(bkp.DeletionTimestamp.IsZero()).To(BeTrue())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
	})

	It("never expires a backup without the annotation", func() {
		r, key, bkp := newCompleted("keeper-backup", "")
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		Expect(bkp.DeletionTimestamp.IsZero()).To(BeTrue())
		Expect(res.RequeueAfter).To(BeZero())
	})
})
