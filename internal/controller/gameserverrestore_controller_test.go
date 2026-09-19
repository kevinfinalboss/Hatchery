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
)

// As with the GameServerBackup tests, there's no Job controller in envtest,
// so these fake a Job's completion by writing its status subresource
// directly rather than letting one really run.
var _ = Describe("GameServerRestore Controller", func() {
	const namespace = "default"
	ctx := context.Background()

	It("locks the target GameServer for the duration and clears it on completion", func() {
		By("creating the target GameServer's PVC and a completed backup for it")
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-target", Namespace: namespace},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				},
			},
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pvc)).To(Succeed()) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: pvc.Name, Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "some-egg"},
				State:   gameserversv1alpha1.GameServerStateStopped,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gs)).To(Succeed()) })

		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "source-backup", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: gs.Name},
				Destination: gameserversv1alpha1.BackupDestination{
					S3: &gameserversv1alpha1.S3Destination{
						Bucket:    "test-bucket",
						SecretRef: corev1.LocalObjectReference{Name: "s3-creds"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())
		bkp.Status.Phase = gameserversv1alpha1.GameServerBackupPhaseCompleted
		Expect(k8sClient.Status().Update(ctx, bkp)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bkp.Name, Namespace: namespace}, bkp)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bkp)).To(Succeed())
		})

		restore := &gameserversv1alpha1.GameServerRestore{
			ObjectMeta: metav1.ObjectMeta{Name: "test-restore", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerRestoreSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: gs.Name},
				BackupRef:     gameserversv1alpha1.GameServerBackupRef{Name: bkp.Name},
			},
		}
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())

		key := types.NamespacedName{Name: restore.Name, Namespace: namespace}
		reconciler := &GameServerRestoreReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, restore)).To(Succeed())
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		})

		By("reconciling: first pass attaches the finalizer")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		By("reconciling: second pass locks the target and creates the restore Job")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gs.Name, Namespace: namespace}, gs)).To(Succeed())
		Expect(gs.Annotations[gameserversv1alpha1.RestoringAnnotation]).To(Equal("true"))

		Expect(k8sClient.Get(ctx, key, restore)).To(Succeed())
		Expect(restore.Status.Phase).To(Equal(gameserversv1alpha1.GameServerRestorePhaseRunning))

		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: restore.Status.JobName, Namespace: namespace}, &job)).To(Succeed())

		By("simulating the restore Job succeeding")
		job.Status.Succeeded = 1
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, restore)).To(Succeed())
		Expect(restore.Status.Phase).To(Equal(gameserversv1alpha1.GameServerRestorePhaseCompleted))
		Expect(restore.Finalizers).To(BeEmpty())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gs.Name, Namespace: namespace}, gs)).To(Succeed())
		Expect(gs.Annotations[gameserversv1alpha1.RestoringAnnotation]).To(BeEmpty())
	})
})
