package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// newTenant builds a Tenant with a small, valid quota. Each spec passes its
// own name: envtest has no namespace controller, so a namespace created by
// one spec stays around forever and names must never be reused.
func newTenant(name string) *gameserversv1alpha1.Tenant {
	return &gameserversv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: gameserversv1alpha1.TenantSpec{
			DisplayName: name,
			Quota: gameserversv1alpha1.TenantQuota{
				CPU:            resource.MustParse("4"),
				Memory:         resource.MustParse("8Gi"),
				Storage:        resource.MustParse("50Gi"),
				MaxGameServers: 3,
			},
		},
	}
}

func newTenantReconciler() *TenantReconciler {
	return &TenantReconciler{
		Client:              k8sClient,
		Scheme:              k8sClient.Scheme(),
		PanelServiceAccount: types.NamespacedName{Namespace: "hatchery-panel", Name: "panel-api"},
	}
}

func reconcileTenant(r *TenantReconciler, name string) {
	GinkgoHelper()
	_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Tenant Controller", func() {
	ctx := context.Background()

	It("provisions the tenant namespace with Pod Security labels and marks the Tenant Active", func() {
		t := newTenant("ns-basic")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		r := newTenantReconciler()

		reconcileTenant(r, t.Name)

		var ns corev1.Namespace
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "hatchery-ns-basic"}, &ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue(gameserversv1alpha1.LabelTenant, "ns-basic"))
		Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/enforce", "baseline"))
		Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/warn", "restricted"))
		Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/audit", "restricted"))

		var got gameserversv1alpha1.Tenant
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: t.Name}, &got)).To(Succeed())
		Expect(got.Finalizers).To(ContainElement(gameserversv1alpha1.TenantFinalizer))
		Expect(got.Status.Phase).To(Equal(gameserversv1alpha1.TenantPhaseActive))
		Expect(got.Status.Namespace).To(Equal("hatchery-ns-basic"))
	})

	It("refuses to adopt a pre-existing namespace it does not own", func() {
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "hatchery-ns-squat"}})).To(Succeed())
		t := newTenant("ns-squat")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())

		_, err := newTenantReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: t.Name}})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not owned by tenant"))

		var got gameserversv1alpha1.Tenant
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: t.Name}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(gameserversv1alpha1.TenantPhaseFailed))
		Expect(got.Status.Namespace).To(BeEmpty(), "a namespace we refused to adopt must not be reported as ours")
	})

	It("rejects the reserved tenant names 'system' and 'catalog' and over-long slugs at admission (CEL)", func() {
		Expect(k8sClient.Create(ctx, newTenant("system"))).NotTo(Succeed())
		Expect(k8sClient.Create(ctx, newTenant("catalog"))).NotTo(Succeed())
		// Valid DNS labels (core validation accepts them), so only our CEL
		// length rule can reject the 33-character one.
		Expect(k8sClient.Create(ctx, newTenant("a23456789012345678901234567890123"))).NotTo(Succeed())
		Expect(k8sClient.Create(ctx, newTenant("b2345678901234567890123456789012"))).To(Succeed())
	})

	It("holds deletion while GameServers remain, then deletes the namespace once empty", func() {
		t := newTenant("ns-del")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		r := newTenantReconciler()
		reconcileTenant(r, t.Name)

		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "hatchery-ns-del"},
			Spec:       gameserversv1alpha1.EggSpec{Images: []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}}, StartCommand: "run"},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		gs := &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "gs", Namespace: "hatchery-ns-del"},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: "e"},
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, gs)).To(Succeed())

		Expect(k8sClient.Delete(ctx, t)).To(Succeed())
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: t.Name}})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).NotTo(BeZero(), "should requeue while a GameServer remains")

		var still gameserversv1alpha1.Tenant
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: t.Name}, &still)).To(Succeed())
		Expect(still.Finalizers).To(ContainElement(gameserversv1alpha1.TenantFinalizer))

		By("removing the GameServer and reconciling again")
		Expect(k8sClient.Delete(ctx, gs)).To(Succeed())
		// The GameServer has no finalizer here (no GameServerReconciler ran), so it is already gone.
		reconcileTenant(r, t.Name)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: t.Name}, &gameserversv1alpha1.Tenant{})).NotTo(Succeed())
		var ns corev1.Namespace
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "hatchery-ns-del"}, &ns)).To(Succeed())
		Expect(ns.DeletionTimestamp).NotTo(BeNil(), "namespace should be Terminating (envtest has no namespace controller to finish it)")
	})

	It("creates the ResourceQuota and LimitRange and puts them back after drift", func() {
		t := newTenant("ns-quota")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		r := newTenantReconciler()
		reconcileTenant(r, t.Name)

		key := types.NamespacedName{Namespace: "hatchery-ns-quota", Name: tenantQuotaName}
		var rq corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, key, &rq)).To(Succeed())
		cpu := rq.Spec.Hard[corev1.ResourceRequestsCPU]
		Expect(cpu.String()).To(Equal("4"))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "hatchery-ns-quota", Name: tenantLimitRangeName}, &corev1.LimitRange{})).To(Succeed())

		By("someone deleting the quota")
		Expect(k8sClient.Delete(ctx, &rq)).To(Succeed())
		reconcileTenant(r, t.Name)
		Expect(k8sClient.Get(ctx, key, &rq)).To(Succeed())

		By("someone inflating the quota")
		rq.Spec.Hard[corev1.ResourceRequestsCPU] = resource.MustParse("999")
		Expect(k8sClient.Update(ctx, &rq)).To(Succeed())
		reconcileTenant(r, t.Name)
		Expect(k8sClient.Get(ctx, key, &rq)).To(Succeed())
		cpu = rq.Spec.Hard[corev1.ResourceRequestsCPU]
		Expect(cpu.String()).To(Equal("4"))

		By("the Tenant's quota being raised")
		var cur gameserversv1alpha1.Tenant
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: t.Name}, &cur)).To(Succeed())
		cur.Spec.Quota.CPU = resource.MustParse("6")
		Expect(k8sClient.Update(ctx, &cur)).To(Succeed())
		reconcileTenant(r, t.Name)
		Expect(k8sClient.Get(ctx, key, &rq)).To(Succeed())
		cpu = rq.Spec.Hard[corev1.ResourceRequestsCPU]
		Expect(cpu.String()).To(Equal("6"))
	})

	It("creates the tenant NetworkPolicies and restores one that was deleted", func() {
		t := newTenant("ns-netpol")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		r := newTenantReconciler()
		reconcileTenant(r, t.Name)

		var list networkingv1.NetworkPolicyList
		Expect(k8sClient.List(ctx, &list, client.InNamespace("hatchery-ns-netpol"))).To(Succeed())
		names := []string{}
		for _, p := range list.Items {
			names = append(names, p.Name)
		}
		Expect(names).To(ConsistOf("default-deny", "allow-dns-egress", "allow-internet-egress", "allow-same-namespace", "allow-panel-sftp"))

		Expect(k8sClient.Delete(ctx, &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: "hatchery-ns-netpol"},
		})).To(Succeed())
		reconcileTenant(r, t.Name)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "hatchery-ns-netpol", Name: "default-deny"}, &networkingv1.NetworkPolicy{})).To(Succeed())
	})

	It("binds the Panel ServiceAccount to hatchery-panel-tenant inside the tenant namespace only", func() {
		t := newTenant("ns-rbac")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		reconcileTenant(newTenantReconciler(), t.Name)

		var rb rbacv1.RoleBinding
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "hatchery-ns-rbac", Name: "hatchery-panel"}, &rb)).To(Succeed())
		Expect(rb.RoleRef.Kind).To(Equal("ClusterRole"))
		Expect(rb.RoleRef.Name).To(Equal("hatchery-panel-tenant"))
		Expect(rb.Subjects).To(ConsistOf(rbacv1.Subject{Kind: "ServiceAccount", Name: "panel-api", Namespace: "hatchery-panel"}))
	})

	It("creates no RoleBinding when no Panel ServiceAccount is configured", func() {
		t := newTenant("ns-norbac")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		r := newTenantReconciler()
		r.PanelServiceAccount = types.NamespacedName{}
		reconcileTenant(r, t.Name)

		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "hatchery-ns-norbac", Name: "hatchery-panel"}, &rbacv1.RoleBinding{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("removes the Panel RoleBinding and sftp policy once the Panel ServiceAccount is unset", func() {
		t := newTenant("ns-prune")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		r := newTenantReconciler()
		reconcileTenant(r, t.Name)

		rbKey := types.NamespacedName{Namespace: "hatchery-ns-prune", Name: "hatchery-panel"}
		npKey := types.NamespacedName{Namespace: "hatchery-ns-prune", Name: "allow-panel-sftp"}
		Expect(k8sClient.Get(ctx, rbKey, &rbacv1.RoleBinding{})).To(Succeed())
		Expect(k8sClient.Get(ctx, npKey, &networkingv1.NetworkPolicy{})).To(Succeed())

		r.PanelServiceAccount = types.NamespacedName{}
		reconcileTenant(r, t.Name)

		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rbKey, &rbacv1.RoleBinding{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, npKey, &networkingv1.NetworkPolicy{}))).To(BeTrue())
	})
})
