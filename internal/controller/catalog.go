package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// panelCatalogClusterRole is bound (by RoleBinding, so it only applies inside
// the catalog namespace) to give the Panel write access to catalog Eggs.
const panelCatalogClusterRole = "hatchery-panel-catalog"

// +kubebuilder:rbac:groups=gameservers.hatchery.io,resources=eggs,verbs=create

// CatalogEnsurer makes sure the global Egg catalog namespace exists and that
// the Panel can write Eggs into it. It runs once when the operator starts
// (see cmd/main.go); everything it does is idempotent.
type CatalogEnsurer struct {
	Client              client.Client
	PanelServiceAccount types.NamespacedName

	// SeedDir holds Egg manifests (*.yaml / *.yml, one or more documents each) to create in the
	// catalog when they are missing — the Helm chart mounts the sample Eggs the installer opted into
	// here. An Egg that already exists is left alone, so edits made through the Panel survive
	// restarts and upgrades. Empty seeds nothing.
	SeedDir string
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

	if err := e.seedEggs(ctx); err != nil {
		return fmt.Errorf("seeding catalog Eggs from %s: %w", e.SeedDir, err)
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

func (e *CatalogEnsurer) seedEggs(ctx context.Context) error {
	if e.SeedDir == "" {
		return nil
	}
	entries, err := os.ReadDir(e.SeedDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		// A mounted ConfigMap also holds "..data"-style bookkeeping entries next to the real keys.
		if strings.HasPrefix(name, "..") || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(e.SeedDir, name))
		if err != nil {
			return err
		}
		dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
		for {
			var egg gameserversv1alpha1.Egg
			if err := dec.Decode(&egg); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return fmt.Errorf("%s: %w", name, err)
			}
			if egg.Name == "" {
				continue // empty document between "---" separators
			}
			egg.Namespace = gameserversv1alpha1.CatalogNamespace
			egg.ResourceVersion = ""
			if err := e.Client.Create(ctx, &egg); err != nil {
				if apierrors.IsAlreadyExists(err) {
					continue
				}
				return fmt.Errorf("%s: creating Egg %s: %w", name, egg.Name, err)
			}
			log.FromContext(ctx).Info("seeded catalog Egg", "egg", egg.Name, "file", name)
		}
	}
	return nil
}
