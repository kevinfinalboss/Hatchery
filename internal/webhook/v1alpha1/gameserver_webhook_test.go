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
	"sigs.k8s.io/controller-runtime/pkg/client"

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

var _ = Describe("GameServer Webhook: variables and displayName", func() {
	const namespace = "default"

	var egg *gameserversv1alpha1.Egg

	BeforeEach(func() {
		egg = &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "webhook-vars-egg", Namespace: namespace},
			Spec: gameserversv1alpha1.EggSpec{
				Image:        "example.com/game:latest",
				StartCommand: "start",
				Variables: []gameserversv1alpha1.EggVariable{
					{Name: "MOTD", UserEditable: true, Default: "hi"},
					{Name: "PORT", UserEditable: true, ValidationRegex: `^[0-9]+$`, Default: "7777"},
					{Name: "LOCKED", UserEditable: false, Default: "x"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, egg) })
	})

	build := func(name string, vars ...gameserversv1alpha1.GameServerVariable) *gameserversv1alpha1.GameServer {
		gs := newWebhookGameServer(name, namespace, egg.Name, gameserversv1alpha1.EggScopeNamespace)
		gs.Spec.Variables = vars
		return gs
	}

	It("rejects an undeclared variable", func() {
		err := k8sClient.Create(ctx, build("vars-unknown", gameserversv1alpha1.GameServerVariable{Name: "NOPE", Value: "1"}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"NOPE" is not declared`))
	})

	It("rejects a variable that is not user-editable", func() {
		err := k8sClient.Create(ctx, build("vars-locked", gameserversv1alpha1.GameServerVariable{Name: "LOCKED", Value: "y"}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"LOCKED" is not editable`))
	})

	It("rejects a value that does not match the validation regex", func() {
		err := k8sClient.Create(ctx, build("vars-regex", gameserversv1alpha1.GameServerVariable{Name: "PORT", Value: "abc"}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"PORT" does not match`))
	})

	It("admits valid variables and a display name", func() {
		gs := build("vars-ok", gameserversv1alpha1.GameServerVariable{Name: "PORT", Value: "25565"})
		gs.Spec.DisplayName = "Survival dos Amigos"
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, gs) })
	})

	It("rejects a display name with a control character", func() {
		gs := build("vars-control")
		gs.Spec.DisplayName = "a\x07b"
		err := k8sClient.Create(ctx, gs)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("control characters"))
	})

	It("does not re-validate variables on an update that leaves them alone", func() {
		gs := build("vars-annotate", gameserversv1alpha1.GameServerVariable{Name: "PORT", Value: "25565"})
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, gs) })

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(egg), egg)).To(Succeed())
		egg.Spec.Variables[1].ValidationRegex = `^9+$`
		Expect(k8sClient.Update(ctx, egg)).To(Succeed())

		gs.Annotations = map[string]string{"example.com/note": "x"}
		Expect(k8sClient.Update(ctx, gs)).To(Succeed())
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
