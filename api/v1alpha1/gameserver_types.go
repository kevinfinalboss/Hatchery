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

// RestartAnnotation asks the controller to restart a running GameServer. Whoever
// wants a restart (the Panel API, or a human via kubectl) sets it to any value
// that differs from the previous one — a timestamp by convention.
const RestartAnnotation = "gameservers.hatchery.io/restart-at"

// SpecHashAnnotation stamps a Pod with a hash of the GameServer spec (variables and resources) it
// was built from, so the controller can tell when the running Pod is out of date.
const SpecHashAnnotation = "gameservers.hatchery.io/spec-hash"

// ConditionRestartRequired is True while the running Pod was built from an older spec.
const ConditionRestartRequired = "RestartRequired"

// ConditionReady is True once the game reported it finished booting (Egg startupDetection). It is
// False with reason StartupTimeout when the regex never matched and the server was treated as up.
const ConditionReady = "Ready"

// ConditionPublicExposureReady is True once a Route was allocated for every port the Egg
// declares. False with reason PoolExhausted when the configured range ran out, or NotConfigured
// when the operator was not started with --public-port-range.
const ConditionPublicExposureReady = "PublicExposureReady"

// GameServerState is the desired lifecycle state of a GameServer, set by whoever
// owns the object (the Panel API, or a human via kubectl).
// +kubebuilder:validation:Enum=Running;Stopped
type GameServerState string

const (
	GameServerStateRunning GameServerState = "Running"
	GameServerStateStopped GameServerState = "Stopped"
)

type EggScope string

const (
	EggScopeNamespace EggScope = "Namespace"
	EggScopeCatalog   EggScope = "Catalog"
)

type GameServerEggRef struct {
	// Name of the Egg resource.
	Name string `json:"name"`

	Scope EggScope `json:"scope,omitempty"`
}

// EggNamespace returns the namespace this GameServer's Egg lives in.
func (g *GameServer) EggNamespace() string {
	if g.Spec.EggRef.Scope == EggScopeCatalog {
		return CatalogNamespace
	}
	return g.Namespace
}

// GameServerVariable overrides the value of a variable declared by the referenced
// Egg for this specific server instance.
type GameServerVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GameServerPublicExposure requests that this server be reachable from outside the cluster
// (e.g. through a homelab's VPS relay), one public port per port the Egg declares. See
// GatewayExposureReconciler and docs/superpowers/specs/2026-09-22-public-game-exposure-design.md.
type GameServerPublicExposure struct {
	// Enabled requests public exposure. Has no effect unless the operator was started with
	// --public-port-range: see the PublicExposureReady condition for why.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// GameServerPublicExposurePort is one Egg-declared port's public allocation.
type GameServerPublicExposurePort struct {
	// Name matches one of the referenced Egg's spec.ports[].name.
	Name string `json:"name"`

	// Port is the public port allocated for it, from the operator's configured range.
	Port int32 `json:"port"`
}

// GameServerPublicExposureStatus reports where this server is publicly reachable, once allocated.
type GameServerPublicExposureStatus struct {
	// Host is the address players use to connect, from the operator's --public-host flag (the
	// operator has no way to discover this on its own — it's whatever DNS name or IP the VPS answers to).
	// +optional
	Host string `json:"host,omitempty"`

	// Ports lists the public port allocated for each of the Egg's declared ports.
	// +listType=map
	// +listMapKey=name
	// +optional
	Ports []GameServerPublicExposurePort `json:"ports,omitempty"`
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

// BackupTarget says where a server's backups go. It is a Panel concern (the Pod never reads it) and
// is what "Create backup" — and, later, scheduled backups — use.
type BackupTarget struct {
	// Connection is the name of one of the organization's S3 connections, or "platform" for the
	// platform's own storage.
	// +kubebuilder:validation:MaxLength=32
	Connection string `json:"connection"`

	// Bucket is one of the connection's buckets. Unused for the platform, which decides it.
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Bucket string `json:"bucket,omitempty"`

	// Prefix is the folder inside the bucket. Empty means the server's name. Unused for the platform.
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Prefix string `json:"prefix,omitempty"`
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

	// DisplayName is the human-friendly name shown in the Panel. Unlike metadata.name (which names
	// the Pod, Service and PVC and never changes), it can be edited at any time.
	// +kubebuilder:validation:MaxLength=64
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// InstallRevision is bumped to make the install script run again ("Reinstall"). The Egg's
	// install script only runs when the marker on the data volume differs from this number.
	// +optional
	InstallRevision int64 `json:"installRevision,omitempty"`

	// Suspended is set by a platform admin (through the Panel) to block a server: its Pod is stopped,
	// its data stays, and it cannot start until unsuspended.
	// +optional
	Suspended bool `json:"suspended,omitempty"`

	// SuspendReason is the admin's explanation, shown to the organization.
	// +kubebuilder:validation:MaxLength=256
	// +optional
	SuspendReason string `json:"suspendReason,omitempty"`

	// StartCommand overrides the Egg's start command for this server. Empty means the Egg's. It may
	// use {{VARIABLE}} placeholders for the Egg's variables and runs as `exec <command>`.
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	StartCommand string `json:"startCommand,omitempty"`

	// BackupTarget is where this server's backups go. Required before its first backup.
	// +optional
	BackupTarget *BackupTarget `json:"backupTarget,omitempty"`

	// PublicExposure requests this server be reachable from outside the cluster. Off by default.
	// +optional
	PublicExposure GameServerPublicExposure `json:"publicExposure,omitempty"`

	// ImageName picks one of the Egg's images by name. Empty means the Egg's first (default) image.
	// +optional
	ImageName string `json:"imageName,omitempty"`

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
	GameServerPhaseStarting   GameServerPhase = "Starting"
	GameServerPhaseSuspended  GameServerPhase = "Suspended"
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

	// PublicExposure reports the public host/ports once allocated by the GatewayExposureReconciler.
	// +optional
	PublicExposure GameServerPublicExposureStatus `json:"publicExposure,omitempty"`

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
