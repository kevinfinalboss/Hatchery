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
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	It("never mounts a ServiceAccount token into the game pod", func() {
		egg := &gameserversv1alpha1.Egg{Spec: gameserversv1alpha1.EggSpec{Image: "example.com/g:1", StartCommand: "run"}}
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "tok", Namespace: "default"},
			Spec:       gameserversv1alpha1.GameServerSpec{Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"}},
		}
		pod, err := buildPod(gs, egg, testSFTPAgentImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.AutomountServiceAccountToken).NotTo(BeNil())
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
	})

	It("opens the Egg's ports with a NetworkPolicy only in tenant namespaces", func() {
		egg := func(ns string) *gameserversv1alpha1.Egg {
			return &gameserversv1alpha1.Egg{
				ObjectMeta: metav1.ObjectMeta{Name: "np-egg", Namespace: ns},
				Spec: gameserversv1alpha1.EggSpec{
					Image:        "example.com/game:latest",
					StartCommand: "run",
					Ports:        []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565}},
				},
			}
		}
		server := func(name, ns string) *gameserversv1alpha1.GameServer {
			return &gameserversv1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: gameserversv1alpha1.GameServerSpec{
					EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "np-egg"},
					State:   gameserversv1alpha1.GameServerStateRunning,
					Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				},
			}
		}
		reconciler := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage}
		reconcileTwice := func(gs *gameserversv1alpha1.GameServer) {
			key := types.NamespacedName{Name: gs.Name, Namespace: gs.Namespace}
			for range 2 {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())
			}
		}

		By("a GameServer in a namespace labeled as a tenant's")
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "hatchery-gs-np", Labels: map[string]string{gameserversv1alpha1.LabelTenant: "gs-np"},
		}})).To(Succeed())
		Expect(k8sClient.Create(ctx, egg("hatchery-gs-np"))).To(Succeed())
		tenantGS := server("gs-np", "hatchery-gs-np")
		Expect(k8sClient.Create(ctx, tenantGS)).To(Succeed())
		reconcileTwice(tenantGS)

		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "hatchery-gs-np", Name: "gs-np-game"}, &np)).To(Succeed())
		Expect(np.Spec.Ingress).To(HaveLen(1))
		Expect(np.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(25565))

		By("a GameServer in an ordinary namespace")
		Expect(k8sClient.Create(ctx, egg(resourceNamespace))).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg(resourceNamespace))).To(Succeed()) })
		plainGS := server("gs-np-default", resourceNamespace)
		Expect(k8sClient.Create(ctx, plainGS)).To(Succeed())
		DeferCleanup(func() {
			deleteAndFinalize(reconciler, plainGS, types.NamespacedName{Name: plainGS.Name, Namespace: resourceNamespace})
		})
		reconcileTwice(plainGS)

		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: resourceNamespace, Name: "gs-np-default-game"}, &networkingv1.NetworkPolicy{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

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

	It("resolves an Egg with scope Catalog from the catalog namespace", func() {
		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: gameserversv1alpha1.CatalogNamespace}})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "ctrl-catalog-egg", Namespace: gameserversv1alpha1.CatalogNamespace},
			Spec:       gameserversv1alpha1.EggSpec{Image: "example.com/catalog-game:1", StartCommand: "run"},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, egg) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "uses-catalog-ctrl", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: egg.Name, Scope: gameserversv1alpha1.EggScopeCatalog},
				State:   gameserversv1alpha1.GameServerStateRunning,
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())

		key := types.NamespacedName{Name: gs.Name, Namespace: resourceNamespace}
		reconciler := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage}
		DeferCleanup(func() { deleteAndFinalize(reconciler, gs, key) })

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}) // finalizer
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		Expect(pod.Spec.Containers[0].Image).To(Equal("example.com/catalog-game:1"))
	})

	It("restarts the Pod exactly once per RestartAnnotation change", func() {
		By("creating the Egg the GameServer will reference")
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "test-egg-restart", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.EggSpec{
				Image:        "example.com/game:latest",
				StartCommand: "start",
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gameserver-restart", Namespace: resourceNamespace},
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

		reconcileOnce := func() {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
		requestRestart := func(value string) {
			Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
			if gs.Annotations == nil {
				gs.Annotations = map[string]string{}
			}
			gs.Annotations[gameserversv1alpha1.RestartAnnotation] = value
			Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		}

		reconcileOnce() // attaches finalizer
		reconcileOnce() // creates resources

		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		firstUID := pod.UID
		Expect(pod.Annotations).NotTo(HaveKey(gameserversv1alpha1.RestartAnnotation))

		By("reconciling again without a restart request leaves the Pod alone")
		reconcileOnce()
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		Expect(pod.UID).To(Equal(firstUID))

		By("requesting a restart deletes the Pod")
		requestRestart("restart-1")
		reconcileOnce()
		Expect(k8sClient.Get(ctx, key, &pod)).NotTo(Succeed())

		By("the next reconcile recreates it, stamped with the request that was just served")
		reconcileOnce()
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		secondUID := pod.UID
		Expect(secondUID).NotTo(Equal(firstUID))
		Expect(pod.Annotations).To(HaveKeyWithValue(gameserversv1alpha1.RestartAnnotation, "restart-1"))

		By("the same request is not served twice")
		reconcileOnce()
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		Expect(pod.UID).To(Equal(secondUID))

		By("a new request restarts it again")
		requestRestart("restart-2")
		reconcileOnce()
		Expect(k8sClient.Get(ctx, key, &pod)).NotTo(Succeed())
		reconcileOnce()
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		Expect(pod.UID).NotTo(Equal(secondUID))
		Expect(pod.Annotations).To(HaveKeyWithValue(gameserversv1alpha1.RestartAnnotation, "restart-2"))
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
