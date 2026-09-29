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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// GameServerRestoreFinalizer guarantees RestoringAnnotation is cleared even if the restore is deleted midway.
const GameServerRestoreFinalizer = "gameservers.hatchery.io/restore-finalizer"

// RestoringAnnotation locks a GameServer during a restore: it cannot start, and a second restore is
// refused. Only the restore controller writes it.
const RestoringAnnotation = "gameservers.hatchery.io/restoring"

// GameServerBackupRef names a GameServerBackup in the same namespace.
type GameServerBackupRef struct {
	Name string `json:"name"`
}

// GameServerRestoreSpec defines the desired state of GameServerRestore
type GameServerRestoreSpec struct {
	// GameServerRef is the target; it must be Stopped (checked by the webhook). Immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="gameServerRef is immutable"
	GameServerRef GameServerRef `json:"gameServerRef"`

	// BackupRef is the GameServerBackup to restore. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="backupRef is immutable"
	BackupRef GameServerBackupRef `json:"backupRef"`
}

// GameServerRestorePhase summarizes where a restore is in its lifecycle.
type GameServerRestorePhase string

const (
	GameServerRestorePhasePending   GameServerRestorePhase = "Pending"
	GameServerRestorePhaseRunning   GameServerRestorePhase = "Running"
	GameServerRestorePhaseCompleted GameServerRestorePhase = "Completed"
	GameServerRestorePhaseFailed    GameServerRestorePhase = "Failed"
)

// GameServerRestoreStatus defines the observed state of GameServerRestore.
type GameServerRestoreStatus struct {
	// Phase summarizes the current lifecycle state of the restore.
	// +optional
	Phase GameServerRestorePhase `json:"phase,omitempty"`

	// JobName is the Job running this restore's restic invocation.
	// +optional
	JobName string `json:"jobName,omitempty"`

	// StartTime is when the restore Job was created.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the restore Job finished, successfully or not.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// conditions are the GameServerRestore's current state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="GameServer",type=string,JSONPath=`.spec.gameServerRef.name`
// +kubebuilder:printcolumn:name="Backup",type=string,JSONPath=`.spec.backupRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GameServerRestore is the Schema for the gameserverrestores API
type GameServerRestore struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of GameServerRestore
	// +required
	Spec GameServerRestoreSpec `json:"spec"`

	// status defines the observed state of GameServerRestore
	// +optional
	Status GameServerRestoreStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GameServerRestoreList contains a list of GameServerRestore
type GameServerRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GameServerRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GameServerRestore{}, &GameServerRestoreList{})
		return nil
	})
}
