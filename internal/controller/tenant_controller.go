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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// tenantDeleteRequeue is how often a Tenant whose deletion is blocked by
// leftover GameServers/backups re-checks whether the namespace has emptied.
const tenantDeleteRequeue = 10 * time.Second

// TenantReconciler provisions and maintains one isolated namespace per Tenant.
type TenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// PanelServiceAccount is the ServiceAccount the Panel API runs as. When
	// set, every tenant namespace gets a RoleBinding granting it the
	// hatchery-panel-tenant ClusterRole there (and only there) and an ingress
	// rule letting its namespace reach the sftp-agent port. Zero value: no
	// binding and no such rule.
	PanelServiceAccount types.NamespacedName

	// EgressExceptCIDRs are extra CIDRs, on top of the private/link-local
	// ranges, that tenant pods may not reach over the internet egress rule.
	EgressExceptCIDRs []string
}

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=tenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=tenants/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=gameserverbackups;gameserverrestores,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas;limitranges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=bind,resourceNames=hatchery-panel-tenant

func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var tenant gameserversv1alpha1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !tenant.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tenant)
	}

	if controllerutil.AddFinalizer(&tenant, gameserversv1alpha1.TenantFinalizer) {
		if err := r.Update(ctx, &tenant); err != nil {
			return ctrl.Result{}, err
		}
	}

	steps := []func(context.Context, *gameserversv1alpha1.Tenant) error{
		r.reconcileNamespace,
		r.reconcileQuota,
		r.reconcileLimitRange,
		r.reconcileNetworkPolicies,
		r.reconcileRoleBinding,
	}
	for i, step := range steps {
		if err := step(ctx, &tenant); err != nil {
			return ctrl.Result{}, r.setStatus(ctx, &tenant, gameserversv1alpha1.TenantPhaseFailed, metav1.ConditionFalse, "ReconcileFailed", err, err.Error())
		}
		if i == 0 {
			// Only once the namespace step succeeded is it ours to report:
			// a namespace we refused to adopt must not show up in status.
			tenant.Status.Namespace = gameserversv1alpha1.TenantNamespace(tenant.Name)
		}
	}
	return ctrl.Result{}, r.setStatus(ctx, &tenant, gameserversv1alpha1.TenantPhaseActive, metav1.ConditionTrue, "Provisioned", nil, "namespace and policies are in place")
}

// reconcileNamespace creates the tenant's namespace, or brings an existing
// one we created back to the desired labels. A namespace with the right name
// that this Tenant does not control is never adopted: without that check, a
// Tenant could take over any pre-existing hatchery-* namespace.
func (r *TenantReconciler) reconcileNamespace(ctx context.Context, t *gameserversv1alpha1.Tenant) error {
	name := gameserversv1alpha1.TenantNamespace(t.Name)

	var ns corev1.Namespace
	err := r.Get(ctx, types.NamespacedName{Name: name}, &ns)
	exists := err == nil
	switch {
	case apierrors.IsNotFound(err):
		ns = corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	case err != nil:
		return err
	case !metav1.IsControlledBy(&ns, t):
		return fmt.Errorf("namespace %q already exists and is not owned by tenant %q", name, t.Name)
	case !ns.DeletionTimestamp.IsZero():
		return fmt.Errorf("namespace %q is terminating", name)
	}

	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}
	ns.Labels[gameserversv1alpha1.LabelTenant] = t.Name
	ns.Labels[gameserversv1alpha1.LabelManagedBy] = gameserversv1alpha1.ManagedByValue
	ns.Labels["pod-security.kubernetes.io/enforce"] = "baseline"
	ns.Labels["pod-security.kubernetes.io/warn"] = "restricted"
	ns.Labels["pod-security.kubernetes.io/audit"] = "restricted"
	if err := controllerutil.SetControllerReference(t, &ns, r.Scheme); err != nil {
		return err
	}

	if !exists {
		return r.Create(ctx, &ns)
	}
	return r.Update(ctx, &ns)
}

// reconcileDelete blocks until the tenant's namespace holds no GameServers,
// backups or restores, then deletes the namespace and lets the Tenant go.
// Backups/restores block too: a GameServerBackup's own finalizer needs to
// create a cleanup Job, which a Terminating namespace refuses.
func (r *TenantReconciler) reconcileDelete(ctx context.Context, t *gameserversv1alpha1.Tenant) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(t, gameserversv1alpha1.TenantFinalizer) {
		return ctrl.Result{}, nil
	}
	nsName := gameserversv1alpha1.TenantNamespace(t.Name)

	var servers gameserversv1alpha1.GameServerList
	if err := r.List(ctx, &servers, client.InNamespace(nsName)); err != nil {
		return ctrl.Result{}, err
	}
	var backups gameserversv1alpha1.GameServerBackupList
	if err := r.List(ctx, &backups, client.InNamespace(nsName)); err != nil {
		return ctrl.Result{}, err
	}
	var restores gameserversv1alpha1.GameServerRestoreList
	if err := r.List(ctx, &restores, client.InNamespace(nsName)); err != nil {
		return ctrl.Result{}, err
	}
	if n := len(servers.Items) + len(backups.Items) + len(restores.Items); n > 0 {
		msg := fmt.Sprintf("deletion blocked: %d GameServer(s), %d backup(s) and %d restore(s) remain in %s",
			len(servers.Items), len(backups.Items), len(restores.Items), nsName)
		if err := r.setStatus(ctx, t, gameserversv1alpha1.TenantPhaseTerminating, metav1.ConditionFalse, "DeletionBlocked", nil, msg); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: tenantDeleteRequeue}, nil
	}

	var ns corev1.Namespace
	err := r.Get(ctx, types.NamespacedName{Name: nsName}, &ns)
	switch {
	case apierrors.IsNotFound(err):
	case err != nil:
		return ctrl.Result{}, err
	case metav1.IsControlledBy(&ns, t) && ns.DeletionTimestamp.IsZero():
		if err := r.Delete(ctx, &ns); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(t, gameserversv1alpha1.TenantFinalizer)
	return ctrl.Result{}, r.Update(ctx, t)
}

// setStatus records phase and the Ready condition. cause is returned as the
// function's result so a failing reconcile step keeps surfacing as an error
// (and gets retried with backoff) after its status has been written.
func (r *TenantReconciler) setStatus(ctx context.Context, t *gameserversv1alpha1.Tenant,
	phase gameserversv1alpha1.TenantPhase, ready metav1.ConditionStatus, reason string, cause error, message string) error {
	t.Status.Phase = phase
	t.Status.ObservedGeneration = t.Generation
	meta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             ready,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: t.Generation,
	})
	if err := r.Status().Update(ctx, t); err != nil {
		return err
	}
	return cause
}

// SetupWithManager sets up the controller with the Manager. Owning the child
// objects is what makes drift (someone deleting or editing the quota, a
// policy, ...) trigger a reconcile that puts them back.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserversv1alpha1.Tenant{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&corev1.LimitRange{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&rbacv1.RoleBinding{}).
		Named("tenant").
		Complete(r)
}

func (r *TenantReconciler) reconcileQuota(ctx context.Context, t *gameserversv1alpha1.Tenant) error {
	rq := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{
		Name: tenantQuotaName, Namespace: gameserversv1alpha1.TenantNamespace(t.Name),
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rq, func() error {
		rq.Spec.Hard = tenantQuotaHard(t.Spec.Quota)
		return controllerutil.SetControllerReference(t, rq, r.Scheme)
	})
	return err
}

func (r *TenantReconciler) reconcileLimitRange(ctx context.Context, t *gameserversv1alpha1.Tenant) error {
	lr := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{
		Name: tenantLimitRangeName, Namespace: gameserversv1alpha1.TenantNamespace(t.Name),
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, lr, func() error {
		lr.Spec = tenantLimitRangeSpec()
		return controllerutil.SetControllerReference(t, lr, r.Scheme)
	})
	return err
}

// pruneIfControlled deletes obj when it exists and is controlled by t. Used
// for objects that are only wanted under some configuration (the Panel
// ServiceAccount being set), so turning that off does not leave them behind.
func (r *TenantReconciler) pruneIfControlled(ctx context.Context, t *gameserversv1alpha1.Tenant, obj client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(obj, t) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, obj))
}

func (r *TenantReconciler) reconcileNetworkPolicies(ctx context.Context, t *gameserversv1alpha1.Tenant) error {
	ns := gameserversv1alpha1.TenantNamespace(t.Name)
	if r.PanelServiceAccount.Namespace == "" {
		stale := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: panelSFTPPolicyName, Namespace: ns}}
		if err := r.pruneIfControlled(ctx, t, stale); err != nil {
			return err
		}
	}
	for _, want := range tenantNetworkPolicies(ns, r.PanelServiceAccount.Namespace, r.EgressExceptCIDRs) {
		got := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: want.Name, Namespace: want.Namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, got, func() error {
			got.Spec = want.Spec
			return controllerutil.SetControllerReference(t, got, r.Scheme)
		}); err != nil {
			return err
		}
	}
	return nil
}

// panelTenantClusterRole is the ClusterRole (config/rbac/panel_tenant_role.yaml)
// bound into each tenant namespace. Binding a ClusterRole with a RoleBinding
// scopes its permissions to that one namespace — the Panel gets no
// cluster-wide access to pods or Secrets.
const panelTenantClusterRole = "hatchery-panel-tenant"

const panelRoleBindingName = "hatchery-panel"

func (r *TenantReconciler) reconcileRoleBinding(ctx context.Context, t *gameserversv1alpha1.Tenant) error {
	sa := r.PanelServiceAccount
	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{
		Name: panelRoleBindingName, Namespace: gameserversv1alpha1.TenantNamespace(t.Name),
	}}
	if sa.Name == "" || sa.Namespace == "" {
		// Panel access was switched off: take back what an earlier run granted.
		return r.pruneIfControlled(ctx, t, rb)
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		rb.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: panelTenantClusterRole}
		rb.Subjects = []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: sa.Namespace}}
		return controllerutil.SetControllerReference(t, rb, r.Scheme)
	})
	return err
}
