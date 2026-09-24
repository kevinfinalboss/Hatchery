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
var gameserverschedulelog = logf.Log.WithName("gameserverschedule-resource")

// SetupGameServerScheduleWebhookWithManager registers the webhook for GameServerSchedule in the manager.
func SetupGameServerScheduleWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &gameserversv1alpha1.GameServerSchedule{}).
		WithValidator(&GameServerScheduleValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-gameservers-hatchery-io-v1alpha1-gameserverschedule,mutating=false,failurePolicy=fail,sideEffects=None,groups=gameservers.hatchery.io,resources=gameserverschedules,verbs=create;update,versions=v1alpha1,name=vgameserverschedule-v1alpha1.kb.io,admissionReviewVersions=v1

// GameServerScheduleValidator admits a GameServerSchedule only when its GameServer exists and its
// spec passes ValidateSchedule (cron/time zone syntax, per-action fields, minimum interval).
type GameServerScheduleValidator struct {
	Client client.Client
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type GameServerSchedule.
func (v *GameServerScheduleValidator) ValidateCreate(ctx context.Context, obj *gameserversv1alpha1.GameServerSchedule) (admission.Warnings, error) {
	gameserverschedulelog.Info("Validation for GameServerSchedule upon creation", "name", obj.GetName())

	var gs gameserversv1alpha1.GameServer
	gsKey := types.NamespacedName{Namespace: obj.Namespace, Name: obj.Spec.GameServerRef.Name}
	if err := v.Client.Get(ctx, gsKey, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("spec.gameServerRef.name: gameserver %q not found", obj.Spec.GameServerRef.Name)
		}
		return nil, err
	}

	if msgs := gameserversv1alpha1.ValidateSchedule(obj.Spec); len(msgs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}

	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type GameServerSchedule.
func (v *GameServerScheduleValidator) ValidateUpdate(_ context.Context, _, newObj *gameserversv1alpha1.GameServerSchedule) (admission.Warnings, error) {
	gameserverschedulelog.Info("Validation for GameServerSchedule upon update", "name", newObj.GetName())

	if msgs := gameserversv1alpha1.ValidateSchedule(newObj.Spec); len(msgs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}

	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type GameServerSchedule.
func (v *GameServerScheduleValidator) ValidateDelete(_ context.Context, obj *gameserversv1alpha1.GameServerSchedule) (admission.Warnings, error) {
	gameserverschedulelog.Info("Validation for GameServerSchedule upon deletion", "name", obj.GetName())
	return nil, nil
}
