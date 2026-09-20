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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// Like the GameServer webhook tests, these go through k8sClient.Create
// against the real envtest admission path rather than calling the validator
// directly.
var _ = Describe("GameServerRestore Webhook", func() {
	const namespace = "default"

	newDestination := func(secretName string) gameserversv1alpha1.BackupDestination {
		return gameserversv1alpha1.BackupDestination{
			S3: &gameserversv1alpha1.S3Destination{
				Bucket:    "test-bucket",
				SecretRef: corev1.LocalObjectReference{Name: secretName},
			},
		}
	}

	newRestore := func(name, gameServerName, backupName string) *gameserversv1alpha1.GameServerRestore {
		return &gameserversv1alpha1.GameServerRestore{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerRestoreSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: gameServerName},
				BackupRef:     gameserversv1alpha1.GameServerBackupRef{Name: backupName},
			},
		}
	}

	// createEgg satisfies the GameServer webhook's own "eggRef must exist"
	// rule so these tests can focus on the GameServerRestore webhook's rules
	// instead.
	createEgg := func(name string) {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       gameserversv1alpha1.EggSpec{Image: "example.com/game:latest", StartCommand: "start"},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })
	}

	It("rejects a restore whose target GameServer does not exist", func() {
		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-backup-1", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: "whatever"},
				Destination:   newDestination("s3-creds"),
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, bkp)).To(Succeed()) })

		err := k8sClient.Create(ctx, newRestore("restore-missing-gs", "does-not-exist", bkp.Name))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("gameserver \"does-not-exist\" not found"))
	})

	It("rejects a restore against a Running GameServer", func() {
		createEgg("restore-webhook-egg-2")
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-running-gs", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "restore-webhook-egg-2"},
				State:   gameserversv1alpha1.GameServerStateRunning,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gs)).To(Succeed()) })

		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-backup-2", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: gs.Name},
				Destination:   newDestination("s3-creds"),
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, bkp)).To(Succeed()) })

		err := k8sClient.Create(ctx, newRestore("restore-against-running", gs.Name, bkp.Name))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be Stopped"))
	})

	It("rejects a restore whose backup has not completed", func() {
		createEgg("restore-webhook-egg-3")
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-stopped-gs", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "restore-webhook-egg-3"},
				State:   gameserversv1alpha1.GameServerStateStopped,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gs)).To(Succeed()) })

		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-backup-3", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: gs.Name},
				Destination:   newDestination("s3-creds"),
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, bkp)).To(Succeed()) })
		// Deliberately left at its default phase (not Completed).

		err := k8sClient.Create(ctx, newRestore("restore-backup-not-done", gs.Name, bkp.Name))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("has not completed"))
	})

	It("admits a restore against a Stopped GameServer with a Completed backup", func() {
		createEgg("restore-webhook-egg-4")
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-happy-gs", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "restore-webhook-egg-4"},
				State:   gameserversv1alpha1.GameServerStateStopped,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gs)).To(Succeed()) })

		bkp := &gameserversv1alpha1.GameServerBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "restore-webhook-backup-4", Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerBackupSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: gs.Name},
				Destination:   newDestination("s3-creds"),
			},
		}
		Expect(k8sClient.Create(ctx, bkp)).To(Succeed())
		bkp.Status.Phase = gameserversv1alpha1.GameServerBackupPhaseCompleted
		Expect(k8sClient.Status().Update(ctx, bkp)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, bkp)).To(Succeed()) })

		restore := newRestore("restore-happy-path", gs.Name, bkp.Name)
		Expect(k8sClient.Create(ctx, restore)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, restore)).To(Succeed()) })
	})
})
