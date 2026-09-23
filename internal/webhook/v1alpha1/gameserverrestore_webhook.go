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
var gameserverrestorelog = logf.Log.WithName("gameserverrestore-resource")

// SetupGameServerRestoreWebhookWithManager registers the webhook for GameServerRestore in the manager.
func SetupGameServerRestoreWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &gameserversv1alpha1.GameServerRestore{}).
		WithValidator(&GameServerRestoreValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-gameservers-hatchery-io-v1alpha1-gameserverrestore,mutating=false,failurePolicy=fail,sideEffects=None,groups=gameservers.hatchery.io,resources=gameserverrestores,verbs=create,versions=v1alpha1,name=vgameserverrestore-v1alpha1.kb.io,admissionReviewVersions=v1

// GameServerRestoreValidator admits a GameServerRestore only against a stopped GameServer and a completed backup.
// The marker above must stay separated from this doc comment by a blank line: attached to a type declaration,
// controller-gen reads it as type documentation and silently drops the webhook from config/webhook/manifests.yaml.
type GameServerRestoreValidator struct {
	Client client.Client
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type GameServerRestore.
func (v *GameServerRestoreValidator) ValidateCreate(ctx context.Context, obj *gameserversv1alpha1.GameServerRestore) (admission.Warnings, error) {
	gameserverrestorelog.Info("Validation for GameServerRestore upon creation", "name", obj.GetName())

	var gs gameserversv1alpha1.GameServer
	gsKey := types.NamespacedName{Namespace: obj.Namespace, Name: obj.Spec.GameServerRef.Name}
	if err := v.Client.Get(ctx, gsKey, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("spec.gameServerRef.name: gameserver %q not found", obj.Spec.GameServerRef.Name)
		}
		return nil, err
	}
	if gs.Spec.State != gameserversv1alpha1.GameServerStateStopped {
		return nil, fmt.Errorf("spec.gameServerRef.name: gameserver %q must be Stopped before it can be restored (current state: %s)",
			gs.Name, gs.Spec.State)
	}
	if gs.Annotations[gameserversv1alpha1.RestoringAnnotation] == "true" {
		return nil, fmt.Errorf("spec.gameServerRef.name: gameserver %q already has a restore in progress", gs.Name)
	}

	var bkp gameserversv1alpha1.GameServerBackup
	backupKey := types.NamespacedName{Namespace: obj.Namespace, Name: obj.Spec.BackupRef.Name}
	if err := v.Client.Get(ctx, backupKey, &bkp); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("spec.backupRef.name: backup %q not found", obj.Spec.BackupRef.Name)
		}
		return nil, err
	}
	if bkp.Status.Phase != gameserversv1alpha1.GameServerBackupPhaseCompleted {
		return nil, fmt.Errorf("spec.backupRef.name: backup %q has not completed (phase: %q)", bkp.Name, bkp.Status.Phase)
	}

	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type GameServerRestore.
func (v *GameServerRestoreValidator) ValidateUpdate(_ context.Context, _, newObj *gameserversv1alpha1.GameServerRestore) (admission.Warnings, error) {
	gameserverrestorelog.Info("Validation for GameServerRestore upon update", "name", newObj.GetName())
	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type GameServerRestore.
func (v *GameServerRestoreValidator) ValidateDelete(_ context.Context, obj *gameserversv1alpha1.GameServerRestore) (admission.Warnings, error) {
	gameserverrestorelog.Info("Validation for GameServerRestore upon deletion", "name", obj.GetName())
	return nil, nil
}
