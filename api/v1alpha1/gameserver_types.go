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

// GameServerFinalizer is set on every GameServer so the controller can clean up
// owned cluster-scoped-ish resources (PVC retention, allocation release) before
// the object is actually removed from etcd.
const GameServerFinalizer = "gameservers.hatchery.io/finalizer"

// GameServerState is the desired lifecycle state of a GameServer, set by whoever
// owns the object (the Panel API, or a human via kubectl).
// +kubebuilder:validation:Enum=Running;Stopped
type GameServerState string

const (
	GameServerStateRunning GameServerState = "Running"
	GameServerStateStopped GameServerState = "Stopped"
)

// GameServerEggRef points at the Egg this GameServer is instantiated from.
type GameServerEggRef struct {
	// Name of the Egg resource in the same namespace.
	Name string `json:"name"`
}

// GameServerVariable overrides the value of a variable declared by the referenced
// Egg for this specific server instance.
type GameServerVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GameServerStorage describes the persistent volume backing the server's data
// directory, mounted by both the server container and the sftp-agent sidecar.
type GameServerStorage struct {
	// Size is the PVC size request, e.g. "10Gi".
	Size string `json:"size"`

	// StorageClassName selects which StorageClass provisions the volume. Empty
	// uses the cluster default.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// GameServerSpec defines the desired state of GameServer
type GameServerSpec struct {
	// EggRef points at the Egg that defines this server's image, start command and
	// install steps. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="eggRef is immutable"
	EggRef GameServerEggRef `json:"eggRef"`

	// State is the desired lifecycle state for this server. The controller drives
	// the Pod towards this state.
	// +kubebuilder:default=Running
	// +optional
	State GameServerState `json:"state,omitempty"`

	// Variables overrides Egg-declared variables for this specific server
	// instance.
	// +optional
	Variables []GameServerVariable `json:"variables,omitempty"`

	// Resources are the compute resource requirements for the server container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Storage describes the persistent volume backing the server's data
	// directory. Immutable after creation; resizing is not supported in the MVP.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="storage is immutable in the MVP"
	Storage GameServerStorage `json:"storage"`
}

// GameServerPhase is a coarse-grained summary of where a GameServer is in its
// lifecycle, intended for humans and simple UI status badges. Controllers and
// webhooks that need fine-grained state should use Conditions instead.
type GameServerPhase string

const (
	GameServerPhasePending    GameServerPhase = "Pending"
	GameServerPhaseInstalling GameServerPhase = "Installing"
	GameServerPhaseRunning    GameServerPhase = "Running"
	GameServerPhaseStopping   GameServerPhase = "Stopping"
	GameServerPhaseStopped    GameServerPhase = "Stopped"
	GameServerPhaseFailed     GameServerPhase = "Failed"
)

// GameServerStatus defines the observed state of GameServer.
type GameServerStatus struct {
	// Phase summarizes the current lifecycle state of the server.
	// +optional
	Phase GameServerPhase `json:"phase,omitempty"`

	// PodName is the name of the Pod currently running this server, when one
	// exists.
	// +optional
	PodName string `json:"podName,omitempty"`

	// ObservedGeneration is the .metadata.generation last reconciled by the
	// controller, used to tell stale status reads apart from current ones.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the GameServer resource.
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
// +kubebuilder:printcolumn:name="Egg",type=string,JSONPath=`.spec.eggRef.name`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.spec.state`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GameServer is the Schema for the gameservers API
type GameServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of GameServer
	// +required
	Spec GameServerSpec `json:"spec"`

	// status defines the observed state of GameServer
	// +optional
	Status GameServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GameServerList contains a list of GameServer
type GameServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GameServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GameServer{}, &GameServerList{})
		return nil
	})
}
