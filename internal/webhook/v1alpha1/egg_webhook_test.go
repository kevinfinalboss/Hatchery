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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// The suite registers the Egg webhook with defaults ["docker.io/itzg", "ghcr.io/ptero-eggs"].
var _ = Describe("Egg Webhook", func() {
	eggWith := func(name, ns string, images ...string) *gameserversv1alpha1.Egg {
		e := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       gameserversv1alpha1.EggSpec{StartCommand: "run"},
		}
		for i, img := range images {
			e.Spec.Images = append(e.Spec.Images, gameserversv1alpha1.EggImage{Name: string(rune('a' + i)), Image: img})
		}
		return e
	}
	tenantNS := func(tenant string, extra ...string) string {
		ns := gameserversv1alpha1.TenantNamespace(tenant)
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: ns, Labels: map[string]string{gameserversv1alpha1.LabelTenant: tenant},
		}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &gameserversv1alpha1.Tenant{
			ObjectMeta: metav1.ObjectMeta{Name: tenant},
			Spec: gameserversv1alpha1.TenantSpec{Quota: gameserversv1alpha1.TenantQuota{
				CPU: resource.MustParse("1"), Memory: resource.MustParse("1Gi"), Storage: resource.MustParse("1Gi"),
				MaxGameServers:       1,
				ExtraImageRegistries: extra,
			}},
		})).To(Succeed())
		return ns
	}

	It("admits a tenant Egg whose images all come from the default list", func() {
		ns := tenantNS("imgok")
		Expect(k8sClient.Create(ctx, eggWith("ok", ns, "itzg/minecraft-server:latest", "ghcr.io/ptero-eggs/yolks:java_21"))).To(Succeed())
	})

	It("rejects a tenant Egg with an image from elsewhere, naming the image", func() {
		ns := tenantNS("imgbad")
		err := k8sClient.Create(ctx, eggWith("bad", ns, "itzgevil/miner:1"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("itzgevil/miner:1"))
	})

	It("checks install and configure images too, but not empty ones", func() {
		ns := tenantNS("imginst")
		e := eggWith("inst", ns, "itzg/minecraft-server")
		e.Spec.Install = &gameserversv1alpha1.EggInstall{Image: "", Script: "true"}
		e.Spec.Configure = &gameserversv1alpha1.EggConfigure{Image: "quay.io/evil/cfg", Script: "true"}
		err := k8sClient.Create(ctx, e)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("quay.io/evil/cfg"))
	})

	It("honours the tenant's extra registries", func() {
		ns := tenantNS("imgextra", "quay.io/myteam")
		Expect(k8sClient.Create(ctx, eggWith("extra", ns, "quay.io/myteam/server:1"))).To(Succeed())
	})

	It("rejects an update that introduces a disallowed image", func() {
		ns := tenantNS("imgupd")
		e := eggWith("upd", ns, "itzg/minecraft-server")
		Expect(k8sClient.Create(ctx, e)).To(Succeed())
		e.Spec.Images[0].Image = "docker.io/library/alpine"
		Expect(k8sClient.Update(ctx, e)).NotTo(Succeed())
	})

	It("never checks catalog Eggs or Eggs outside tenant namespaces", func() {
		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: gameserversv1alpha1.CatalogNamespace}})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(k8sClient.Create(ctx, eggWith("anything", gameserversv1alpha1.CatalogNamespace, "quay.io/whatever/x"))).To(Succeed())
		Expect(k8sClient.Create(ctx, eggWith("plain", "default", "quay.io/whatever/x"))).To(Succeed())
	})
})
