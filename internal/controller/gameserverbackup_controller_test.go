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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/backup"
)

// There's no kubelet or Job controller in envtest, so a real restic Job
// never actually runs here. These tests fake that out the same way most
// controller test suites do: write directly to the Job's status subresource
// to simulate what a real cluster's Job controller would report once a Pod
// finishes, and check the GameServerBackupController reacts correctly.
var _ = Describe("GameServerBackup Controller", func() {
	const namespace = "default"
	ctx := context.Background()

	newDestination := func(secretName string) gameserversv1alpha1.BackupDestination {
		return gameserversv1alpha1.BackupDestination{
			S3: &gameserversv1alpha1.S3Destination{
				Bucket:    "test-bucket",
				Prefix:    "gameservers/test",
				SecretRef: corev1.LocalObjectReference{Name: secretName},
			},
		}
	}

	It("runs a backup Job to completion and cleans it up on delete", func() {
		By("creating the PVC the backup will reference")
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-target", Namespace: namespace},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pvc)).To(Succeed()) })

		By("creating the GameServerBackup")
		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: pvc.Name},
				Destination:   newDestination("s3-creds"),
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())

		key := types.NamespacedName{Name: bkp.Name, Namespace: namespace}
		reconciler := &GameServerBackupReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		By("reconciling: first pass attaches the finalizer")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		By("reconciling: second pass creates the backup Job")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		Expect(bkp.Status.Phase).To(Equal(gameserversv1alpha1.GameServerBackupPhaseRunning))
		Expect(bkp.Status.JobName).To(Equal(bkp.Name))

		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bkp.Status.JobName, Namespace: namespace}, &job)).To(Succeed())
		Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal(backup.Image))
		Expect(envValue(job.Spec.Template.Spec.Containers[0].Env, "TAG")).To(Equal(backup.Tag(bkp.Name)))

		By("simulating the Job succeeding")
		job.Status.Succeeded = 1
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		Expect(bkp.Status.Phase).To(Equal(gameserversv1alpha1.GameServerBackupPhaseCompleted))
		Expect(bkp.Status.CompletionTime).NotTo(BeNil())

		By("deleting: first pass creates the cleanup Job")
		Expect(k8sClient.Delete(ctx, bkp)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		var cleanupJob batchv1.Job
		cleanupKey := types.NamespacedName{Name: backup.CleanupJobName(bkp.Name), Namespace: namespace}
		Expect(k8sClient.Get(ctx, cleanupKey, &cleanupJob)).To(Succeed())

		By("simulating the cleanup Job succeeding, which releases the finalizer")
		cleanupJob.Status.Succeeded = 1
		Expect(k8sClient.Status().Update(ctx, &cleanupJob)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, &gameserversv1alpha1.GameServerBackup{})).NotTo(Succeed())
	})

	It("fails fast when the target GameServer has no data volume", func() {
		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-no-pvc", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: "does-not-exist"},
				Destination:   newDestination("s3-creds"),
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())

		key := types.NamespacedName{Name: bkp.Name, Namespace: namespace}
		reconciler := &GameServerBackupReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, bkp)).To(Succeed())
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		})

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}) // finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}) // fails
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, bkp)).To(Succeed())
		Expect(bkp.Status.Phase).To(Equal(gameserversv1alpha1.GameServerBackupPhaseFailed))
	})
})

func envValue(env []corev1.EnvVar, name string) string {
	for _, e := range env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}
