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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// These exercise the real admission path (k8sClient.Create against the
// envtest API server, which routes through the webhook server started in
// webhook_suite_test.go) rather than calling the validator's methods
// directly, so a wiring mistake in SetupGameServerWebhookWithManager or the
// +kubebuilder:webhook marker would fail these tests too.
var _ = Describe("GameServer Webhook", func() {
	const namespace = "default"

	newGameServer := func(name, eggName string) *gameserversv1alpha1.GameServer {
		return &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: eggName},
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
	}

	It("rejects a GameServer whose eggRef does not exist", func() {
		gs := newGameServer("webhook-missing-egg", "does-not-exist")
		err := k8sClient.Create(ctx, gs)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("egg \"does-not-exist\" not found"))
	})

	It("admits a GameServer whose eggRef exists", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "webhook-valid-egg", Namespace: namespace},
			Spec: gameserversv1alpha1.EggSpec{
				Image:        "example.com/game:latest",
				StartCommand: "start",
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		gs := newGameServer("webhook-valid-gameserver", egg.Name)
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gs)).To(Succeed()) })
	})

	It("rejects starting a GameServer while a restore is in progress", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "webhook-restoring-egg", Namespace: namespace},
			Spec: gameserversv1alpha1.EggSpec{
				Image:        "example.com/game:latest",
				StartCommand: "start",
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, egg)).To(Succeed()) })

		gs := newGameServer("webhook-restoring-gameserver", egg.Name)
		gs.Spec.State = gameserversv1alpha1.GameServerStateStopped
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, gs)).To(Succeed()) })

		gs.Annotations = map[string]string{gameserversv1alpha1.RestoringAnnotation: "true"}
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())

		gs.Spec.State = gameserversv1alpha1.GameServerStateRunning
		err := k8sClient.Update(ctx, gs)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("restore is in progress"))
	})

	It("accepts a GameServer that references a catalog Egg and rejects a missing one", func() {
		ensureNamespace("hatchery-catalog")
		catalogEgg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "catalog-egg", Namespace: "hatchery-catalog"},
			Spec:       gameserversv1alpha1.EggSpec{Image: "example.com/g:1", StartCommand: "run"},
		}
		Expect(k8sClient.Create(ctx, catalogEgg)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, catalogEgg) })

		ok := newWebhookGameServer("uses-catalog", "default", "catalog-egg", gameserversv1alpha1.EggScopeCatalog)
		Expect(k8sClient.Create(ctx, ok)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ok) })

		missing := newWebhookGameServer("uses-missing-catalog", "default", "no-such-egg", gameserversv1alpha1.EggScopeCatalog)
		err := k8sClient.Create(ctx, missing)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("catalog"))
	})

	It("does not resolve a private-scope eggRef against the catalog", func() {
		ensureNamespace("hatchery-catalog")
		only := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "only-in-catalog", Namespace: "hatchery-catalog"},
			Spec:       gameserversv1alpha1.EggSpec{Image: "example.com/g:1", StartCommand: "run"},
		}
		Expect(k8sClient.Create(ctx, only)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, only) })

		gs := newWebhookGameServer("wrong-scope", "default", "only-in-catalog", gameserversv1alpha1.EggScopeNamespace)
		Expect(k8sClient.Create(ctx, gs)).NotTo(Succeed())
	})
})

func ensureNamespace(name string) {
	err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func newWebhookGameServer(name, ns, egg string, scope gameserversv1alpha1.EggScope) *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: gameserversv1alpha1.GameServerSpec{
			EggRef:  gameserversv1alpha1.GameServerEggRef{Name: egg, Scope: scope},
			Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
		},
	}
}
