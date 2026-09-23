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

// ScheduleAction is what one task of a schedule does.
// +kubebuilder:validation:Enum=Command;Restart;Start;Stop;Backup
type ScheduleAction string

const (
	ScheduleActionCommand ScheduleAction = "Command"
	ScheduleActionRestart ScheduleAction = "Restart"
	ScheduleActionStart   ScheduleAction = "Start"
	ScheduleActionStop    ScheduleAction = "Stop"
	ScheduleActionBackup  ScheduleAction = "Backup"
)

// ScheduleTask is one step of a schedule run. Tasks run in order; DelaySeconds is waited before it.
type ScheduleTask struct {
	Action ScheduleAction `json:"action"`

	// Command is written to the game's console. Only for action Command.
	// +kubebuilder:validation:MaxLength=512
	// +optional
	Command string `json:"command,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=900
	// +optional
	DelaySeconds int32 `json:"delaySeconds,omitempty"`

	// KeepLast keeps only the newest N completed backups this schedule made (0 keeps all). Only
	// for action Backup; manual backups are never rotated.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	KeepLast int32 `json:"keepLast,omitempty"`
}

// GameServerScheduleSpec defines the desired state of GameServerSchedule
type GameServerScheduleSpec struct {
	// GameServerRef names the GameServer this schedule acts on. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="gameServerRef is immutable"
	GameServerRef GameServerRef `json:"gameServerRef"`

	// +kubebuilder:validation:MaxLength=64
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Cron is a standard 5-field expression (minute hour day-of-month month day-of-week).
	Cron string `json:"cron"`

	// TimeZone is an IANA name (e.g. America/Sao_Paulo). Empty means UTC.
	// +optional
	TimeZone string `json:"timeZone,omitempty"`

	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// OnlyWhenRunning skips the whole run while the server is not Running.
	// +kubebuilder:default=true
	// +optional
	OnlyWhenRunning bool `json:"onlyWhenRunning"`

	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Tasks []ScheduleTask `json:"tasks"`
}

// ScheduleRunResult summarizes how a schedule run ended.
// +kubebuilder:validation:Enum=Succeeded;Failed;Skipped
type ScheduleRunResult string

const (
	ScheduleRunSucceeded ScheduleRunResult = "Succeeded"
	ScheduleRunFailed    ScheduleRunResult = "Failed"
	ScheduleRunSkipped   ScheduleRunResult = "Skipped"
)

// ScheduleRun records the outcome of the most recent schedule run.
type ScheduleRun struct {
	StartedAt metav1.Time `json:"startedAt"`
	// +optional
	FinishedAt *metav1.Time      `json:"finishedAt,omitempty"`
	Result     ScheduleRunResult `json:"result"`
	// +optional
	Message string `json:"message,omitempty"`
}

// ActiveScheduleRun is a run in progress. Kept in status so an operator restart resumes it at
// TaskIndex instead of repeating earlier tasks.
type ActiveScheduleRun struct {
	// ID identifies the run (its start time, RFC3339); backups it creates carry it so a retried
	// Backup task never creates a second one.
	ID         string      `json:"id"`
	StartedAt  metav1.Time `json:"startedAt"`
	TaskIndex  int32       `json:"taskIndex"`
	NextTaskAt metav1.Time `json:"nextTaskAt"`
}

// GameServerScheduleStatus defines the observed state of GameServerSchedule.
type GameServerScheduleStatus struct {
	// +optional
	NextScheduleTime *metav1.Time `json:"nextScheduleTime,omitempty"`
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	// +optional
	LastRun *ScheduleRun `json:"lastRun,omitempty"`
	// +optional
	ActiveRun *ActiveScheduleRun `json:"activeRun,omitempty"`
	// LastRunNow is the RunNowAnnotation value already acted on (one manual run per value).
	// +optional
	LastRunNow string `json:"lastRunNow,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Server",type=string,JSONPath=`.spec.gameServerRef.name`
// +kubebuilder:printcolumn:name="Cron",type=string,JSONPath=`.spec.cron`
// +kubebuilder:printcolumn:name="Next",type=date,JSONPath=`.status.nextScheduleTime`
// +kubebuilder:printcolumn:name="Last",type=string,JSONPath=`.status.lastRun.result`

// GameServerSchedule runs a list of tasks against one GameServer on a cron schedule.
type GameServerSchedule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of GameServerSchedule
	// +required
	Spec GameServerScheduleSpec `json:"spec"`

	// status defines the observed state of GameServerSchedule
	// +optional
	Status GameServerScheduleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GameServerScheduleList contains a list of GameServerSchedule
type GameServerScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GameServerSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GameServerSchedule{}, &GameServerScheduleList{})
		return nil
	})
}
