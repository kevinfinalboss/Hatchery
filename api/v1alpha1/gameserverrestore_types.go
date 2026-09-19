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

// GameServerRestoreFinalizer is set on every GameServerRestore so the
// controller can guarantee RestoringAnnotation gets cleared off the target
// GameServer even if the GameServerRestore is deleted mid-flight.
const GameServerRestoreFinalizer = "gameservers.hatchery.io/restore-finalizer"

// RestoringAnnotation is set to "true" on a GameServer by whichever
// GameServerRestore currently targets it, for the duration of the restore.
// It's what the GameServerRestore validating webhook checks to refuse a
// second concurrent restore against the same GameServer, and it's a lock in
// the literal sense: the GameServerRestoreController is the only writer, and
// it always clears it — on completion, on failure, or via the finalizer if
// the GameServerRestore is deleted early.
const RestoringAnnotation = "gameservers.hatchery.io/restoring"

// GameServerBackupRef names a GameServerBackup in the same namespace.
type GameServerBackupRef struct {
	Name string `json:"name"`
}

// GameServerRestoreSpec defines the desired state of GameServerRestore
type GameServerRestoreSpec struct {
	// GameServerRef is the GameServer the backup is restored onto. It must be
	// Stopped at admission time (enforced by the validating webhook, not CEL,
	// since checking another resource's state isn't something a CRD schema
	// can express). Immutable after creation.
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

	// conditions represent the current state of the GameServerRestore resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
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
