package controller

import (
	"os"
	"path/filepath"

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

	It("seeds Eggs from SeedDir only when they are missing", func() {
		dir := GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(dir, "..2026_09_23"), 0o755)).To(Succeed()) // ConfigMap bookkeeping entry
		Expect(os.WriteFile(filepath.Join(dir, "eggs.yaml"), []byte(`apiVersion: gameservers.hatchery.io/v1alpha1
kind: Egg
metadata:
  name: seed-a
  namespace: somewhere-else
spec:
  images: [{name: default, image: example.com/a:1}]
  startCommand: run-a
---
apiVersion: gameservers.hatchery.io/v1alpha1
kind: Egg
metadata:
  name: seed-b
spec:
  images: [{name: default, image: example.com/b:1}]
  startCommand: run-b
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not an egg"), 0o644)).To(Succeed())

		e := &CatalogEnsurer{Client: k8sClient, SeedDir: dir}
		Expect(e.Ensure(ctx)).To(Succeed())

		var a gameserversv1alpha1.Egg
		keyA := types.NamespacedName{Namespace: gameserversv1alpha1.CatalogNamespace, Name: "seed-a"}
		Expect(k8sClient.Get(ctx, keyA, &a)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: gameserversv1alpha1.CatalogNamespace, Name: "seed-b"}, &gameserversv1alpha1.Egg{})).To(Succeed())

		// An edit made after seeding (e.g. through the Panel) must survive the next operator start.
		a.Spec.StartCommand = "edited"
		Expect(k8sClient.Update(ctx, &a)).To(Succeed())
		Expect(e.Ensure(ctx)).To(Succeed())
		Expect(k8sClient.Get(ctx, keyA, &a)).To(Succeed())
		Expect(a.Spec.StartCommand).To(Equal("edited"))
	})
})
