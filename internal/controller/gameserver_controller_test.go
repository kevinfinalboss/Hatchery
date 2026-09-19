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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

const testSFTPAgentImage = "example.com/sftp-agent:test"

var _ = Describe("GameServer Controller", func() {
	const resourceNamespace = "default"

	ctx := context.Background()

	// deleteAndFinalize deletes gs and drives the reconciler until its
	// finalizer has been processed and the object is actually gone. There's
	// no running controller manager in these tests, so nothing but an
	// explicit Reconcile call would ever pick the deletion up.
	deleteAndFinalize := func(reconciler *GameServerReconciler, gs *gameserversv1alpha1.GameServer, key types.NamespacedName) {
		Expect(k8sClient.Delete(ctx, gs)).To(Succeed())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, &gameserversv1alpha1.GameServer{})).NotTo(Succeed())
	}

	It("creates the owned Pod, Service and PVC for a Running GameServer", func() {
		By("creating the Egg the GameServer will reference")
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "test-egg", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.EggSpec{
				Image:        "example.com/game:latest",
				StartCommand: "start --port {{PORT}}",
				Variables: []gameserversv1alpha1.EggVariable{
					{Name: "PORT", Default: "25565"},
				},
				Ports: []gameserversv1alpha1.EggPort{
					{Name: "game", ContainerPort: 25565, Protocol: corev1.ProtocolTCP},
				},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		By("creating the GameServer")
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gameserver", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: egg.Name},
				State:   gameserversv1alpha1.GameServerStateRunning,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())

		key := types.NamespacedName{Name: gs.Name, Namespace: resourceNamespace}
		reconciler := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage}
		DeferCleanup(func() { deleteAndFinalize(reconciler, gs, key) })

		By("reconciling the GameServer (first pass just attaches the finalizer)")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		By("reconciling again to create the owned resources")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		By("verifying the owned Pod was created with the resolved start command")
		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		Expect(pod.Spec.Containers).To(HaveLen(2))
		Expect(pod.Spec.Containers[0].Image).To(Equal(egg.Spec.Image))
		Expect(pod.Spec.Containers[0].Command).To(ContainElement("exec start --port 25565"))
		Expect(pod.Spec.Containers[0].Stdin).To(BeTrue())

		By("verifying the sftp-agent sidecar was injected")
		Expect(pod.Spec.Containers[1].Name).To(Equal("sftp-agent"))
		Expect(pod.Spec.Containers[1].Image).To(Equal(testSFTPAgentImage))

		By("verifying the Service exposes the Egg's ports and the sftp port")
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, key, &svc)).To(Succeed())
		Expect(svc.Spec.Ports).To(HaveLen(2))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(25565)))
		Expect(svc.Spec.Ports[1].Name).To(Equal("sftp"))

		By("verifying the data PVC was created")
		var pvc corev1.PersistentVolumeClaim
		Expect(k8sClient.Get(ctx, key, &pvc)).To(Succeed())

		By("verifying the sftp-agent HMAC secret was created")
		var secret corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: resourceNamespace, Name: gs.Name + "-sftp"}, &secret)).To(Succeed())
		Expect(secret.Data["hmac-key"]).To(HaveLen(32))

		By("verifying status was updated")
		var updated gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, key, &updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhasePending))
		Expect(updated.Status.PodName).To(Equal(gs.Name))
		Expect(updated.Finalizers).To(ContainElement(gameserversv1alpha1.GameServerFinalizer))
	})

	It("deletes the Pod but keeps the PVC when the desired state is Stopped", func() {
		By("creating the Egg the GameServer will reference")
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "test-egg-stop", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.EggSpec{
				Image:        "example.com/game:latest",
				StartCommand: "start",
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gameserver-stop", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: egg.Name},
				State:   gameserversv1alpha1.GameServerStateRunning,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())

		key := types.NamespacedName{Name: gs.Name, Namespace: resourceNamespace}
		reconciler := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage}
		DeferCleanup(func() { deleteAndFinalize(reconciler, gs, key) })

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}) // attaches finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}) // creates resources
		Expect(err).NotTo(HaveOccurred())

		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())

		By("switching the desired state to Stopped")
		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, &pod)).NotTo(Succeed())

		var pvc corev1.PersistentVolumeClaim
		Expect(k8sClient.Get(ctx, key, &pvc)).To(Succeed())

		var updated gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, key, &updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseStopped))
	})
})
