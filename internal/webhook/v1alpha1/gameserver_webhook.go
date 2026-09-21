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
	"unicode"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var gameserverlog = logf.Log.WithName("gameserver-resource")

// SetupGameServerWebhookWithManager registers the webhook for GameServer in the manager.
func SetupGameServerWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &gameserversv1alpha1.GameServer{}).
		WithValidator(&GameServerValidator{Client: mgr.GetClient()}).
		Complete()
}

// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-gameservers-hatchery-io-v1alpha1-gameserver,mutating=false,failurePolicy=fail,sideEffects=None,groups=gameservers.hatchery.io,resources=gameservers,verbs=create;update,versions=v1alpha1,name=vgameserver-v1alpha1.kb.io,admissionReviewVersions=v1

// GameServerValidator validates GameServer resources on create and update.
// Immutability rules (eggRef, storage) live as CEL x-kubernetes-validations on
// the CRD schema itself — cheaper for the API server to enforce and requiring
// no round-trip to this webhook. What's left for the webhook is the one thing
// CEL can't check: whether a referenced object actually exists.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type GameServerValidator struct {
	Client client.Client
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type GameServer.
func (v *GameServerValidator) ValidateCreate(ctx context.Context, obj *gameserversv1alpha1.GameServer) (admission.Warnings, error) {
	gameserverlog.Info("Validation for GameServer upon creation", "name", obj.GetName())
	egg, err := v.lookupEgg(ctx, obj)
	if err != nil {
		return nil, err
	}
	if err := validateDisplayName(obj.Spec.DisplayName); err != nil {
		return nil, err
	}
	if err := validateImage(egg, obj); err != nil {
		return nil, err
	}
	if err := validateStartCommand(egg, obj); err != nil {
		return nil, err
	}
	if obj.Spec.Suspended && obj.Spec.State == gameserversv1alpha1.GameServerStateRunning {
		return nil, fmt.Errorf("spec.state: a suspended GameServer cannot be Running")
	}
	return nil, validateVariables(egg, obj)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type GameServer.
func (v *GameServerValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *gameserversv1alpha1.GameServer) (admission.Warnings, error) {
	gameserverlog.Info("Validation for GameServer upon update", "name", newObj.GetName())
	// eggRef is immutable (enforced via CEL on the CRD), so if it existed at
	// create time it still refers to the same object; no need to re-check here.

	// RestoringAnnotation is set by a GameServerRestoreController for the
	// duration of a restore (see internal/controller and
	// gameserverrestore_webhook.go, which is what stops a *new* restore from
	// starting against an already-Running server). This is the other half:
	// stopping this GameServer from starting back up while a restore already
	// in progress is still writing to its data volume.
	if newObj.Spec.State == gameserversv1alpha1.GameServerStateRunning &&
		newObj.Annotations[gameserversv1alpha1.RestoringAnnotation] == "true" {
		return nil, fmt.Errorf("spec.state: cannot start this GameServer while a restore is in progress")
	}
	if newObj.Spec.Suspended && newObj.Spec.State == gameserversv1alpha1.GameServerStateRunning {
		return nil, fmt.Errorf("spec.state: cannot start this GameServer while it is suspended")
	}
	if err := validateDisplayName(newObj.Spec.DisplayName); err != nil {
		return nil, err
	}
	// Variables and the image are only re-checked when they changed: the controller updates
	// finalizers and annotations on this object, and those must not start failing because the Egg
	// moved on.
	varsChanged := !equality.Semantic.DeepEqual(oldObj.Spec.Variables, newObj.Spec.Variables)
	imageChanged := oldObj.Spec.ImageName != newObj.Spec.ImageName
	cmdChanged := oldObj.Spec.StartCommand != newObj.Spec.StartCommand
	if varsChanged || imageChanged || cmdChanged {
		egg, err := v.lookupEgg(ctx, newObj)
		if err != nil {
			return nil, err
		}
		if imageChanged {
			if err := validateImage(egg, newObj); err != nil {
				return nil, err
			}
		}
		if cmdChanged || varsChanged {
			// A variable can be removed from the Egg-declared set the command refers to, so the
			// command is re-checked whenever either changes.
			if err := validateStartCommand(egg, newObj); err != nil {
				return nil, err
			}
		}
		if varsChanged {
			return nil, validateVariables(egg, newObj)
		}
	}
	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type GameServer.
func (v *GameServerValidator) ValidateDelete(_ context.Context, obj *gameserversv1alpha1.GameServer) (admission.Warnings, error) {
	gameserverlog.Info("Validation for GameServer upon deletion", "name", obj.GetName())
	// Nothing to validate on delete; the webhook isn't even registered for the
	// delete verb (see the +kubebuilder:webhook marker above).
	return nil, nil
}

func validateDisplayName(name string) error {
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("spec.displayName: must not contain control characters")
		}
	}
	return nil
}

func validateStartCommand(egg *gameserversv1alpha1.Egg, gs *gameserversv1alpha1.GameServer) error {
	if msgs := gameserversv1alpha1.ValidateStartCommand(egg, gs.Spec.StartCommand); len(msgs) > 0 {
		return fmt.Errorf("spec.startCommand: %s", strings.Join(msgs, "; "))
	}
	return nil
}

func validateImage(egg *gameserversv1alpha1.Egg, gs *gameserversv1alpha1.GameServer) error {
	if _, ok := egg.ResolveImage(gs.Spec.ImageName); !ok {
		return fmt.Errorf("spec.imageName: egg %q does not declare image %q", egg.Name, gs.Spec.ImageName)
	}
	return nil
}

func validateVariables(egg *gameserversv1alpha1.Egg, gs *gameserversv1alpha1.GameServer) error {
	if msgs := gameserversv1alpha1.ValidateVariableOverrides(egg, gs.Spec.Variables); len(msgs) > 0 {
		return fmt.Errorf("spec.variables: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// lookupEgg fetches the Egg the GameServer references, with an admission-friendly error when it
// does not exist.
func (v *GameServerValidator) lookupEgg(ctx context.Context, gs *gameserversv1alpha1.GameServer) (*gameserversv1alpha1.Egg, error) {
	var egg gameserversv1alpha1.Egg
	ns := gs.EggNamespace()
	key := types.NamespacedName{Namespace: ns, Name: gs.Spec.EggRef.Name}
	where := fmt.Sprintf("namespace %q", ns)
	if gs.Spec.EggRef.Scope == gameserversv1alpha1.EggScopeCatalog {
		where = "the global catalog"
	}
	if err := v.Client.Get(ctx, key, &egg); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("spec.eggRef.name: egg %q not found in %s", gs.Spec.EggRef.Name, where)
		}
		return nil, fmt.Errorf("spec.eggRef.name: looking up egg %q: %w", gs.Spec.EggRef.Name, err)
	}
	return &egg, nil
}
