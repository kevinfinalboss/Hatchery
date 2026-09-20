package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

var _ = Describe("Catalog namespace", func() {
	It("creates hatchery-catalog and binds the Panel's write access, idempotently", func() {
		e := &CatalogEnsurer{
			Client:              k8sClient,
			PanelServiceAccount: types.NamespacedName{Namespace: "hatchery-panel", Name: "panel-api"},
		}
		Expect(e.Ensure(ctx)).To(Succeed())
		Expect(e.Ensure(ctx)).To(Succeed())

		var ns corev1.Namespace
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gameserversv1alpha1.CatalogNamespace}, &ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/enforce", "restricted"))

		var rb rbacv1.RoleBinding
		key := types.NamespacedName{Namespace: gameserversv1alpha1.CatalogNamespace, Name: "hatchery-panel-catalog"}
		Expect(k8sClient.Get(ctx, key, &rb)).To(Succeed())
		Expect(rb.RoleRef.Name).To(Equal("hatchery-panel-catalog"))
		Expect(rb.Subjects).To(ConsistOf(rbacv1.Subject{Kind: "ServiceAccount", Name: "panel-api", Namespace: "hatchery-panel"}))
	})
})
