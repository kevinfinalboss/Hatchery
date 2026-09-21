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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
		egg := &gameserversv1alpha1.Egg{Spec: gameserversv1alpha1.EggSpec{Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run"}}
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
					Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:latest"}},
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
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:latest"}},
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
		Expect(pod.Spec.Containers[0].Image).To(Equal(egg.Spec.Images[0].Image))
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
			Spec:       gameserversv1alpha1.EggSpec{Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/catalog-game:1"}}, StartCommand: "run"},
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
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:latest"}},
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

	It("runs the image the GameServer picks, and the Egg's first one by default", func() {
		egg := &gameserversv1alpha1.Egg{Spec: gameserversv1alpha1.EggSpec{
			Images:       []gameserversv1alpha1.EggImage{{Name: "Java 25", Image: "img:25"}, {Name: "Java 17", Image: "img:17"}},
			StartCommand: "run",
			Install:      &gameserversv1alpha1.EggInstall{Script: "true"},
		}}
		server := func(imageName string) *gameserversv1alpha1.GameServer {
			return &gameserversv1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Name: "img", Namespace: "default"},
				Spec: gameserversv1alpha1.GameServerSpec{
					ImageName: imageName,
					Storage:   gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				},
			}
		}

		def, err := buildPod(server(""), egg, testSFTPAgentImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(def.Spec.Containers[0].Image).To(Equal("img:25"))
		Expect(def.Spec.InitContainers[0].Image).To(Equal("img:25"), "install falls back to the chosen image")

		picked, err := buildPod(server("Java 17"), egg, testSFTPAgentImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(picked.Spec.Containers[0].Image).To(Equal("img:17"))

		egg.Spec.Install.Image = "installer:1"
		withInstaller, err := buildPod(server("Java 17"), egg, testSFTPAgentImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(withInstaller.Spec.InitContainers[0].Image).To(Equal("installer:1"), "an explicit install image wins")
	})

	It("fails a GameServer whose imageName the Egg does not declare", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "test-egg-badimg", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.EggSpec{
				Images:       []gameserversv1alpha1.EggImage{{Name: "only", Image: "img:1"}},
				StartCommand: "run",
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gameserver-badimg", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:    gameserversv1alpha1.GameServerEggRef{Name: egg.Name},
				State:     gameserversv1alpha1.GameServerStateRunning,
				ImageName: "gone",
				Storage:   gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		key := types.NamespacedName{Name: gs.Name, Namespace: resourceNamespace}
		reconciler := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), SFTPAgentImage: testSFTPAgentImage}
		DeferCleanup(func() { deleteAndFinalize(reconciler, gs, key) })

		for range 2 {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		Expect(gs.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseFailed))
		Expect(k8sClient.Get(ctx, key, &corev1.Pod{})).NotTo(Succeed(), "no Pod may be created from an unknown image")
	})

	Describe("pod lifecycle wiring", func() {
		newServer := func() *gameserversv1alpha1.GameServer {
			return &gameserversv1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Name: "life", Namespace: "default"},
				Spec:       gameserversv1alpha1.GameServerSpec{Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"}},
			}
		}
		newEgg := func() *gameserversv1alpha1.Egg {
			return &gameserversv1alpha1.Egg{Spec: gameserversv1alpha1.EggSpec{
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "img:1"}},
				StartCommand: "run",
			}}
		}
		serverOf := func(pod *corev1.Pod) corev1.Container { return pod.Spec.Containers[0] }
		envOf := func(c corev1.Container, name string) (string, bool) {
			for _, e := range c.Env {
				if e.Name == name {
					return e.Value, true
				}
			}
			return "", false
		}

		It("stops through the stopCommand and gives the Egg's grace period", func() {
			egg := newEgg()
			egg.Spec.StopCommand = "stop"
			egg.Spec.StopTimeoutSeconds = ptr.To(int32(120))
			pod, err := buildPod(newServer(), egg, testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())

			Expect(*pod.Spec.TerminationGracePeriodSeconds).To(Equal(int64(120)))
			hook := serverOf(pod).Lifecycle.PreStop.Exec.Command
			Expect(hook[len(hook)-1]).To(ContainSubstring("/proc/1/fd/0"))
			Expect(hook[len(hook)-1]).To(ContainSubstring("kill -0 1"))
			v, ok := envOf(serverOf(pod), "HATCHERY_STOP_COMMAND")
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal("stop"))
		})

		It("defaults the grace period to 60s and adds no hook when there is nothing to send", func() {
			pod, err := buildPod(newServer(), newEgg(), testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(*pod.Spec.TerminationGracePeriodSeconds).To(Equal(int64(60)))
			Expect(serverOf(pod).Lifecycle).To(BeNil())
		})

		It("sends a non-default stopSignal itself, since lifecycle.stopSignal needs a feature gate", func() {
			egg := newEgg()
			egg.Spec.StopSignal = "SIGINT"
			pod, err := buildPod(newServer(), egg, testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())
			hook := serverOf(pod).Lifecycle.PreStop.Exec.Command
			Expect(hook[len(hook)-1]).To(ContainSubstring("kill -INT 1"))

			egg.Spec.StopSignal = "SIGTERM"
			pod, err = buildPod(newServer(), egg, testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(serverOf(pod).Lifecycle).To(BeNil(), "SIGTERM is what the kubelet sends anyway")

			egg.Spec.StopSignal = "INT; rm -rf /"
			pod, err = buildPod(newServer(), egg, testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())
			Expect(serverOf(pod).Lifecycle).To(BeNil(), "an unsafe signal name is ignored, never interpolated")
		})

		It("wraps the install script so it only runs once per installRevision", func() {
			egg := newEgg()
			egg.Spec.Install = &gameserversv1alpha1.EggInstall{Entrypoint: []string{"bash", "-c"}, Script: "echo hi"}
			gs := newServer()
			gs.Spec.InstallRevision = 3
			pod, err := buildPod(gs, egg, testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())

			install := pod.Spec.InitContainers[0]
			Expect(install.Name).To(Equal("install"))
			Expect(install.Command[:2]).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(install.Command[2]).To(ContainSubstring(".hatchery-installed"))
			Expect(install.Command[len(install.Command)-3:]).To(Equal([]string{"bash", "-c", "echo hi"}), "the Egg's own entrypoint and script are passed through untouched")
			v, ok := envOf(install, "HATCHERY_INSTALL_REVISION")
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal("3"))
		})

		It("runs the configure step after install, on the game image by default", func() {
			egg := newEgg()
			egg.Spec.Install = &gameserversv1alpha1.EggInstall{Script: "true"}
			egg.Spec.Configure = &gameserversv1alpha1.EggConfigure{Script: "echo conf"}
			pod, err := buildPod(newServer(), egg, testSFTPAgentImage)
			Expect(err).NotTo(HaveOccurred())

			Expect(pod.Spec.InitContainers).To(HaveLen(2))
			configure := pod.Spec.InitContainers[1]
			Expect(configure.Name).To(Equal("configure"))
			Expect(configure.Image).To(Equal("img:1"))
			Expect(configure.Command).To(Equal([]string{"/bin/sh", "-c", "echo conf"}))
		})
	})

	It("hashes only what needs a restart to take effect", func() {
		a := &gameserversv1alpha1.GameServer{}
		a.Spec.Variables = []gameserversv1alpha1.GameServerVariable{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}

		reordered := a.DeepCopy()
		reordered.Spec.Variables = []gameserversv1alpha1.GameServerVariable{{Name: "B", Value: "2"}, {Name: "A", Value: "1"}}
		Expect(specHash(reordered)).To(Equal(specHash(a)), "variable order must not change the hash")

		changed := a.DeepCopy()
		changed.Spec.Variables[0].Value = "3"
		Expect(specHash(changed)).NotTo(Equal(specHash(a)))

		resized := a.DeepCopy()
		resized.Spec.Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
		Expect(specHash(resized)).NotTo(Equal(specHash(a)))

		reimaged := a.DeepCopy()
		reimaged.Spec.ImageName = "Java 17"
		Expect(specHash(reimaged)).NotTo(Equal(specHash(a)), "a different image needs a restart")

		renamed := a.DeepCopy()
		renamed.Spec.DisplayName = "renamed"
		Expect(specHash(renamed)).To(Equal(specHash(a)), "renaming never needs a restart")
	})

	It("flags RestartRequired when the spec changes under a running Pod, and clears it after a restart", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "test-egg-pending", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.EggSpec{
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:latest"}},
				StartCommand: "start {{MOTD}}",
				Variables:    []gameserversv1alpha1.EggVariable{{Name: "MOTD", Default: "hi", UserEditable: true}},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gameserver-pending", Namespace: resourceNamespace},
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
		restartRequired := func() metav1.ConditionStatus {
			Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
			cond := apimeta.FindStatusCondition(gs.Status.Conditions, gameserversv1alpha1.ConditionRestartRequired)
			Expect(cond).NotTo(BeNil())
			return cond.Status
		}

		reconcileOnce() // finalizer
		reconcileOnce() // resources
		var pod corev1.Pod
		Expect(k8sClient.Get(ctx, key, &pod)).To(Succeed())
		Expect(pod.Annotations).To(HaveKey(gameserversv1alpha1.SpecHashAnnotation))
		Expect(restartRequired()).To(Equal(metav1.ConditionFalse))

		By("changing a variable while the Pod runs")
		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		gs.Spec.Variables = []gameserversv1alpha1.GameServerVariable{{Name: "MOTD", Value: "new"}}
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce()
		Expect(restartRequired()).To(Equal(metav1.ConditionTrue))

		By("the Pod is not touched until someone restarts it")
		var same corev1.Pod
		Expect(k8sClient.Get(ctx, key, &same)).To(Succeed())
		Expect(same.UID).To(Equal(pod.UID))

		By("a restart recreates the Pod from the new spec and clears the flag")
		Expect(k8sClient.Get(ctx, key, gs)).To(Succeed())
		gs.Annotations = map[string]string{gameserversv1alpha1.RestartAnnotation: "r1"}
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
		reconcileOnce() // deletes the Pod
		reconcileOnce() // recreates it
		Expect(restartRequired()).To(Equal(metav1.ConditionFalse))
	})

	It("deletes the Pod but keeps the PVC when the desired state is Stopped", func() {
		By("creating the Egg the GameServer will reference")
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "test-egg-stop", Namespace: resourceNamespace},
			Spec: gameserversv1alpha1.EggSpec{
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:latest"}},
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
		Expect(updated.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseStopping), "the reconcile that deletes the Pod reports Stopping")

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, &updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(gameserversv1alpha1.GameServerPhaseStopped))
	})
})
