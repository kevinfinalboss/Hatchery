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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// GameServerRef names a GameServer in the same namespace.
type GameServerRef struct {
	Name string `json:"name"`
}

// BackupExpiresAtAnnotation (RFC 3339): the operator deletes the backup after that time. Set by the
// Panel on backups kept on the platform's storage.
const BackupExpiresAtAnnotation = "gameservers.hatchery.io/expires-at"

// BackupGameServerLabel and BackupDestinationLabel let the Panel list an organization's backups by
// server and by where they are stored.
const (
	BackupGameServerLabel  = "gameservers.hatchery.io/gameserver"
	BackupDestinationLabel = "gameservers.hatchery.io/backup-destination"
)

// GameServerBackupFinalizer removes the remote snapshots before the backup object goes away.
const GameServerBackupFinalizer ="gameservers.hatchery.io/backup-finalizer"

// S3Destination is an S3-compatible bucket.
type S3Destination struct {
	// Endpoint is the S3-compatible host, e.g. "s3.amazonaws.com" or a
	// MinIO/R2 endpoint. Defaults to "s3.amazonaws.com" when empty.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Bucket is the destination bucket name.
	Bucket string `json:"bucket"`

	// Prefix separates this server's restic repository inside the bucket, e.g. "gameservers/my-server".
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// SecretRef names a Secret with "access-key", "secret-key" and "restic-password" (the
	// password must never change for a repository).
	SecretRef corev1.LocalObjectReference `json:"secretRef"`
}

// BackupDestination is where a backup is stored (only S3 today).
type BackupDestination struct {
	S3 *S3Destination `json:"s3"`
}

// GameServerBackupSpec defines the desired state of GameServerBackup
type GameServerBackupSpec struct {
	// GameServerRef names the GameServer whose data volume this backup
	// captures. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="gameServerRef is immutable"
	GameServerRef GameServerRef `json:"gameServerRef"`

	// Destination says where the backup data is stored. Immutable after
	// creation — changing it would orphan whatever was already backed up.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="destination is immutable"
	Destination BackupDestination `json:"destination"`
}

// GameServerBackupPhase summarizes where a backup is in its lifecycle.
type GameServerBackupPhase string

const (
	GameServerBackupPhasePending   GameServerBackupPhase = "Pending"
	GameServerBackupPhaseRunning   GameServerBackupPhase = "Running"
	GameServerBackupPhaseCompleted GameServerBackupPhase = "Completed"
	GameServerBackupPhaseFailed    GameServerBackupPhase = "Failed"
	GameServerBackupPhaseDeleting  GameServerBackupPhase = "Deleting"
)

// Condition types and reasons of a GameServerBackup's world-save pause (Egg.spec.backup).
const (
	BackupConditionQuiesced = "Quiesced"
	BackupConditionResumed  = "Resumed"

	QuiesceReasonSaved      = "Saved"
	QuiesceReasonDelay      = "Delay"
	QuiesceReasonTimeout    = "Timeout"
	QuiesceReasonSendFailed = "SendFailed"

	ResumeReasonSent          = "Sent"
	ResumeReasonPodReplaced   = "PodReplaced"
	ResumeReasonNothingToSend = "NothingToSend"
)

// BackupQuiesceStatus records the world-save pause around this backup so a restarted operator
// resumes it instead of sending the commands twice.
type BackupQuiesceStatus struct {
	// PodUID is the game Pod the Before commands were sent to; After goes only to that same Pod.
	PodUID string `json:"podUID"`
	// BeforeSentAt is when the Before commands were (about to be) sent.
	// +optional
	BeforeSentAt *metav1.Time `json:"beforeSentAt,omitempty"`
	// ResumedAt is when the After step ended: sent, skipped, or given up.
	// +optional
	ResumedAt *metav1.Time `json:"resumedAt,omitempty"`
	// ResumeAttempts counts failed attempts at sending After.
	// +optional
	ResumeAttempts int32 `json:"resumeAttempts,omitempty"`
}

// GameServerBackupStatus defines the observed state of GameServerBackup.
type GameServerBackupStatus struct {
	// Phase summarizes the current lifecycle state of the backup.
	// +optional
	Phase GameServerBackupPhase `json:"phase,omitempty"`

	// JobName is the Job currently (or most recently) running this backup's
	// restic invocation.
	// +optional
	JobName string `json:"jobName,omitempty"`

	// StartTime is when the backup Job was created.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the backup Job finished, successfully or not.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Quiesce tracks the world-save pause of a backup of a running server.
	// +optional
	Quiesce *BackupQuiesceStatus `json:"quiesce,omitempty"`

	// conditions are the GameServerBackup's current state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="GameServer",type=string,JSONPath=`.spec.gameServerRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GameServerBackup is the Schema for the gameserverbackups API
type GameServerBackup struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of GameServerBackup
	// +required
	Spec GameServerBackupSpec `json:"spec"`

	// status defines the observed state of GameServerBackup
	// +optional
	Status GameServerBackupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GameServerBackupList contains a list of GameServerBackup
type GameServerBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GameServerBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GameServerBackup{}, &GameServerBackupList{})
		return nil
	})
}
