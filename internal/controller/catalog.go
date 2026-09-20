package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// panelCatalogClusterRole is bound (by RoleBinding, so it only applies inside
// the catalog namespace) to give the Panel write access to catalog Eggs.
const panelCatalogClusterRole = "hatchery-panel-catalog"

// CatalogEnsurer makes sure the global Egg catalog namespace exists and that
// the Panel can write Eggs into it. It runs once when the operator starts
// (see cmd/main.go); everything it does is idempotent.
type CatalogEnsurer struct {
	Client              client.Client
	PanelServiceAccount types.NamespacedName
}

func (e *CatalogEnsurer) Ensure(ctx context.Context) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: gameserversv1alpha1.CatalogNamespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, e.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[gameserversv1alpha1.LabelManagedBy] = gameserversv1alpha1.ManagedByValue
		// Nothing ever runs in the catalog namespace (it only holds Egg objects),
		// so the strictest Pod Security level costs nothing.
		ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
		return nil
	}); err != nil {
		return err
	}

	sa := e.PanelServiceAccount
	if sa.Name == "" || sa.Namespace == "" {
		return nil
	}
	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{
		Name: panelCatalogClusterRole, Namespace: gameserversv1alpha1.CatalogNamespace,
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, rb, func() error {
		rb.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: panelCatalogClusterRole}
		rb.Subjects = []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: sa.Namespace}}
		return nil
	})
	return err
}
