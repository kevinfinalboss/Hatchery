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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

var _ = Describe("GatewayExposureReconciler", func() {
	const ns = "default"
	ctx := context.Background()

	newReconciler := func() *GatewayExposureReconciler {
		return &GatewayExposureReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			PortRangeMin: 30000, PortRangeMax: 30002,
			PublicHost:  "game.example.com",
			GatewayName: "hatchery-public", GatewayNamespace: ns, GatewayClassName: "cilium",
		}
	}

	It("does nothing for a GameServer with public exposure disabled", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-off-egg", Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run",
				Ports: []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565}},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-off", Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "gwe-off-egg"},
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())

		_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: gs.Name, Namespace: ns}})
		Expect(err).NotTo(HaveOccurred())

		var got gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gs.Name, Namespace: ns}, &got)).To(Succeed())
		Expect(got.Status.PublicExposure.Ports).To(BeEmpty())

		var route gatewayv1.TCPRoute
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gwe-off-game", Namespace: ns}, &route)).NotTo(Succeed())
	})

	It("allocates a public port and creates a TCPRoute when exposure is enabled", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-on-egg", Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run",
				Ports: []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565}},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-on", Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:         gameserversv1alpha1.GameServerEggRef{Name: "gwe-on-egg"},
				Storage:        gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				PublicExposure: gameserversv1alpha1.GameServerPublicExposure{Enabled: true},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())

		key := types.NamespacedName{Name: gs.Name, Namespace: ns}
		_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		var got gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
		Expect(got.Status.PublicExposure.Ports).To(HaveLen(1))
		Expect(got.Status.PublicExposure.Ports[0]).To(Equal(gameserversv1alpha1.GameServerPublicExposurePort{Name: "game", Port: 30000}))
		Expect(got.Status.PublicExposure.Host).To(Equal("game.example.com"))
		cond := apimeta.FindStatusCondition(got.Status.Conditions, gameserversv1alpha1.ConditionPublicExposureReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))

		var route gatewayv1.TCPRoute
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gwe-on-game", Namespace: ns}, &route)).To(Succeed())
		Expect(route.Spec.Rules[0].BackendRefs[0].Name).To(Equal(gatewayv1.ObjectName("gwe-on")))
		Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(25565)))

		var gw gatewayv1.Gateway
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "hatchery-public", Namespace: ns}, &gw)).To(Succeed())
		Expect(gw.Spec.Listeners).To(ContainElement(HaveField("Port", gatewayv1.PortNumber(30000))))
	})

	It("removes the TCPRoute and clears status when exposure is disabled again", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-off2-egg", Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run",
				Ports: []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565}},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-off2", Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:         gameserversv1alpha1.GameServerEggRef{Name: "gwe-off2-egg"},
				Storage:        gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				PublicExposure: gameserversv1alpha1.GameServerPublicExposure{Enabled: true},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		key := types.NamespacedName{Name: gs.Name, Namespace: ns}
		reconciler := newReconciler()
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		var got gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
		got.Spec.PublicExposure.Enabled = false
		Expect(k8sClient.Update(ctx, &got)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, &got)).To(Succeed())
		Expect(got.Status.PublicExposure.Ports).To(BeEmpty())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gwe-off2-game", Namespace: ns}, &gatewayv1.TCPRoute{})).NotTo(Succeed())
	})

	It("sets PoolExhausted and allocates nothing when the range is full", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-full-egg", Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run",
				Ports: []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565}},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		reconciler := &GatewayExposureReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			PortRangeMin: 31000, PortRangeMax: 31000, // exactly one slot
			PublicHost: "game.example.com", GatewayName: "hatchery-public-full", GatewayNamespace: ns, GatewayClassName: "cilium",
		}
		first := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-full-1", Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef: gameserversv1alpha1.GameServerEggRef{Name: "gwe-full-egg"}, Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				PublicExposure: gameserversv1alpha1.GameServerPublicExposure{Enabled: true},
			},
		}
		second := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-full-2", Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef: gameserversv1alpha1.GameServerEggRef{Name: "gwe-full-egg"}, Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				PublicExposure: gameserversv1alpha1.GameServerPublicExposure{Enabled: true},
			},
		}
		Expect(k8sClient.Create(ctx, first)).To(Succeed())
		Expect(k8sClient.Create(ctx, second)).To(Succeed())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: first.Name, Namespace: ns}})
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: second.Name, Namespace: ns}})
		Expect(err).NotTo(HaveOccurred())

		var gotSecond gameserversv1alpha1.GameServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: second.Name, Namespace: ns}, &gotSecond)).To(Succeed())
		Expect(gotSecond.Status.PublicExposure.Ports).To(BeEmpty())
		cond := apimeta.FindStatusCondition(gotSecond.Status.Conditions, gameserversv1alpha1.ConditionPublicExposureReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("PoolExhausted"))
	})

	It("drops the Listener from the shared Gateway once the GameServer is fully deleted", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-del-egg", Namespace: ns},
			Spec: gameserversv1alpha1.EggSpec{
				Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run",
				Ports: []gameserversv1alpha1.EggPort{{Name: "game", ContainerPort: 25565}},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gwe-del", Namespace: ns},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:         gameserversv1alpha1.GameServerEggRef{Name: "gwe-del-egg"},
				Storage:        gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
				PublicExposure: gameserversv1alpha1.GameServerPublicExposure{Enabled: true},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		key := types.NamespacedName{Name: gs.Name, Namespace: ns}
		reconciler := newReconciler()
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		var gw gatewayv1.Gateway
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "hatchery-public", Namespace: ns}, &gw)).To(Succeed())
		Expect(gw.Spec.Listeners).To(ContainElement(HaveField("Name", gatewayv1.SectionName("gwe-del-game"))))

		// No finalizer on this GameServer for this test env, so Delete removes it outright — the
		// real regression is that a bare Get-returns-NotFound Reconcile used to return before ever
		// calling rebuildGateway, leaving this Listener orphaned forever.
		Expect(k8sClient.Delete(ctx, gs)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, &gameserversv1alpha1.GameServer{})).NotTo(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, types.NamespacedName{Name: "hatchery-public", Namespace: ns}, &gw)
		if err == nil {
			Expect(gw.Spec.Listeners).NotTo(ContainElement(HaveField("Name", gatewayv1.SectionName("gwe-del-game"))))
		} else {
			Expect(client.IgnoreNotFound(err)).To(Succeed()) // Gateway deleted outright: also correct, no listeners left at all.
		}
	})
})
