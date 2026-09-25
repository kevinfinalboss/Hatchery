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
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// SetupEggWebhookWithManager registers the Egg image-origin check. defaultRegistries is the
// platform list (--allowed-image-registries); empty turns the check off.
func SetupEggWebhookWithManager(mgr ctrl.Manager, defaultRegistries []string) error {
	return ctrl.NewWebhookManagedBy(mgr, &gameserversv1alpha1.Egg{}).
		WithValidator(&EggValidator{Client: mgr.GetClient(), DefaultRegistries: defaultRegistries}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-gameservers-hatchery-io-v1alpha1-egg,mutating=false,failurePolicy=fail,sideEffects=None,groups=gameservers.hatchery.io,resources=eggs,verbs=create;update,versions=v1alpha1,name=vegg-v1alpha1.kb.io,admissionReviewVersions=v1

// EggValidator keeps an organization's private Eggs to images from allowed registries: the
// platform's defaults plus the organization's own extras (Tenant.spec.quota.extraImageRegistries).
// Catalog Eggs and Eggs outside tenant namespaces are the platform admin's and are not checked.
type EggValidator struct {
	Client            client.Client
	DefaultRegistries []string
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type Egg.
func (v *EggValidator) ValidateCreate(ctx context.Context, obj *gameserversv1alpha1.Egg) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type Egg.
func (v *EggValidator) ValidateUpdate(ctx context.Context, _, newObj *gameserversv1alpha1.Egg) (admission.Warnings, error) {
	return nil, v.validate(ctx, newObj)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type Egg.
func (v *EggValidator) ValidateDelete(context.Context, *gameserversv1alpha1.Egg) (admission.Warnings, error) {
	return nil, nil
}

func (v *EggValidator) validate(ctx context.Context, egg *gameserversv1alpha1.Egg) error {
	// Spec rules (startup regex, mods block) apply everywhere, the catalog included.
	if msgs := egg.Spec.Validate(); len(msgs) > 0 {
		return fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	if len(v.DefaultRegistries) == 0 || egg.Namespace == gameserversv1alpha1.CatalogNamespace {
		return nil
	}
	allowed, isTenant, err := AllowedRegistriesFor(ctx, v.Client, egg.Namespace, v.DefaultRegistries)
	if err != nil {
		return err
	}
	if !isTenant {
		return nil
	}
	var denied []string
	for _, img := range egg.Spec.ImageRefs() {
		if !gameserversv1alpha1.ImageAllowed(img, allowed) {
			denied = append(denied, img)
		}
	}
	if len(denied) > 0 {
		return fmt.Errorf("image(s) %s are not from an allowed registry (allowed: %s)",
			strings.Join(denied, ", "), strings.Join(allowed, ", "))
	}
	return nil
}

// AllowedRegistriesFor returns the defaults plus the extras of the Tenant owning namespace ns, and
// whether ns belongs to a tenant at all. A missing Tenant object means "defaults only".
func AllowedRegistriesFor(ctx context.Context, c client.Reader, ns string, defaults []string) ([]string, bool, error) {
	var namespace corev1.Namespace
	if err := c.Get(ctx, types.NamespacedName{Name: ns}, &namespace); err != nil {
		return nil, false, err
	}
	tenantName := namespace.Labels[gameserversv1alpha1.LabelTenant]
	if tenantName == "" {
		return nil, false, nil
	}
	allowed := append([]string{}, defaults...)
	var tenant gameserversv1alpha1.Tenant
	if err := c.Get(ctx, types.NamespacedName{Name: tenantName}, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return allowed, true, nil
		}
		return nil, true, err
	}
	return append(allowed, tenant.Spec.Quota.ExtraImageRegistries...), true, nil
}
