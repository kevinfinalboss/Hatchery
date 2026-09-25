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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EggVariable declares one environment variable that a server built from this Egg
// understands. GameServers may override the value; unset required variables without
// a Default are rejected by the GameServer validating webhook.
type EggVariable struct {
	// Name is the environment variable name injected into the server container.
	Name string `json:"name"`

	// Description explains what this variable controls, shown to end users.
	// +optional
	Description string `json:"description,omitempty"`

	// Default is used when a GameServer does not override this variable.
	// +optional
	Default string `json:"default,omitempty"`

	// Required marks whether a GameServer must set this variable explicitly when it
	// has no Default.
	// +optional
	Required bool `json:"required,omitempty"`

	// UserEditable controls whether end users, not just admins, may change this
	// value on a GameServer.
	// +optional
	UserEditable bool `json:"userEditable,omitempty"`

	// ValidationRegex, when set, is applied against the resolved value before it is
	// accepted.
	// +optional
	ValidationRegex string `json:"validationRegex,omitempty"`
}

// EggPort declares one network port a server built from this Egg listens on.
type EggPort struct {
	// Name identifies this port, e.g. "game", "query" or "rcon".
	Name string `json:"name"`

	// ContainerPort is the port the process listens on inside the container.
	ContainerPort int32 `json:"containerPort"`

	// Protocol is TCP or UDP.
	// +kubebuilder:validation:Enum=TCP;UDP
	// +kubebuilder:default=TCP
	// +optional
	Protocol corev1.Protocol `json:"protocol,omitempty"`

	// Default marks the primary allocation exposed to players, used when a
	// GameServer does not pick one explicitly.
	// +optional
	Default bool `json:"default,omitempty"`
}

// EggInstall describes the one-off step that prepares server files before the
// first start. The GameServer controller runs this as an initContainer against
// the same data volume the server container mounts.
type EggInstall struct {
	// Image used to run the install script container. Defaults to the Egg's main
	// Image when empty.
	// +optional
	Image string `json:"image,omitempty"`

	// Entrypoint overrides the image entrypoint for the install step.
	// +optional
	Entrypoint []string `json:"entrypoint,omitempty"`

	// Script is the shell script executed to install or prepare the server files.
	Script string `json:"script"`
}

// EggSpec defines the desired state of Egg
// EggResources are the resources the Panel pre-fills in the create-server form for an Egg.
type EggResources struct {
	CPU    *resource.Quantity `json:"cpu,omitempty"`
	Memory *resource.Quantity `json:"memory,omitempty"`
	Disk   *resource.Quantity `json:"disk,omitempty"`
}

// EggImage is one container image an Egg can run its server with, under a name the user picks by
// (e.g. "Java 21"): the game version often dictates the runtime image.
type EggImage struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Image string `json:"image"`
}

// EggStartupDetection tells the operator how to recognise that the game finished booting: a regex
// matched against the console output.
type EggStartupDetection struct {
	// Regex is matched against the recent console output (RE2 syntax).
	// +kubebuilder:validation:MinLength=1
	Regex string `json:"regex"`

	// TimeoutSeconds is how long to wait for a match before treating the server as running anyway
	// (Ready=False, reason StartupTimeout). Defaults to 600.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
}

// EggConfigure is a step that runs on every start, after install and before the game, to (re)write
// configuration files from the server's variables. It must not download anything.
type EggConfigure struct {
	// Image defaults to the image the server runs with.
	// +optional
	Image string `json:"image,omitempty"`
	// Entrypoint defaults to ["/bin/sh", "-c"].
	// +optional
	Entrypoint []string `json:"entrypoint,omitempty"`
	// Script is passed to the entrypoint as its last argument.
	// +kubebuilder:validation:MinLength=1
	Script string `json:"script"`
}

// EggModsKind says what the mod installer puts in EggMods.Directory.
// +kubebuilder:validation:Enum=plugin;mod
type EggModsKind string

const (
	EggModsPlugin EggModsKind = "plugin"
	EggModsMod    EggModsKind = "mod"
)

// KnownModLoaders are the loader names the mod installer understands (Modrinth's names).
var KnownModLoaders = []string{"paper", "purpur", "spigot", "bukkit", "folia", "fabric", "quilt", "forge", "neoforge"}

// EggModsGameVersion says where the running game version comes from: Variable when it holds a
// concrete version, else File (written by the install script with the version it downloaded).
type EggModsGameVersion struct {
	// +optional
	Variable string `json:"variable,omitempty"`
	// +optional
	File string `json:"file,omitempty"`
}

// EggMods turns on the Panel's mod/plugin installer for servers of this Egg.
type EggMods struct {
	Kind EggModsKind `json:"kind"`
	// Loaders the installed files must target, in Modrinth's names (see KnownModLoaders).
	// +kubebuilder:validation:MinItems=1
	Loaders []string `json:"loaders"`
	// Directory is relative to the server's data directory, e.g. "plugins".
	Directory string `json:"directory"`
	// +optional
	GameVersion EggModsGameVersion `json:"gameVersion,omitempty"`
}

type EggSpec struct {
	// Images are the container images the game server process can run with. The first is the
	// default; a GameServer picks another by name through spec.imageName.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	Images []EggImage `json:"images"`

	// StartCommand starts the server process. It may reference variables declared
	// below using {{VARIABLE_NAME}} placeholders, resolved by the GameServer
	// controller before the container starts.
	StartCommand string `json:"startCommand"`

	// StopCommand, when set, is written to the server process's stdin to request a
	// graceful shutdown before StopSignal is used.
	// +optional
	StopCommand string `json:"stopCommand,omitempty"`

	// StopSignal is the OS signal sent to the container when StopCommand is empty
	// or does not terminate the process within the grace period, e.g. "SIGTERM".
	// +kubebuilder:default=SIGTERM
	// +optional
	StopSignal string `json:"stopSignal,omitempty"`

	// Install describes how to prepare the server files before the first start.
	// Eggs with pre-baked images (no install step) may omit this.
	// +optional
	Install *EggInstall `json:"install,omitempty"`

	// Variables declares the environment variables this Egg understands.
	// +optional
	Variables []EggVariable `json:"variables,omitempty"`

	// Ports declares the network ports this Egg's process listens on.
	// +optional
	Ports []EggPort `json:"ports,omitempty"`

	// StopTimeoutSeconds is how long the server gets to stop gracefully (StopCommand or StopSignal)
	// before it is killed. Defaults to 60.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	// +optional
	StopTimeoutSeconds *int32 `json:"stopTimeoutSeconds,omitempty"`

	// StartupDetection lets the Panel tell "starting" from "online".
	// +optional
	StartupDetection *EggStartupDetection `json:"startupDetection,omitempty"`

	// Configure runs on every start after Install; see EggConfigure.
	// +optional
	Configure *EggConfigure `json:"configure,omitempty"`

	// RecommendedResources pre-fills the create-server form; the user may change them.
	// +optional
	RecommendedResources *EggResources `json:"recommendedResources,omitempty"`

	// Mods turns on the mod/plugin installer for servers of this Egg.
	// +optional
	Mods *EggMods `json:"mods,omitempty"`

	// RunAsUser runs the game (and the configure step) as this uid and gid instead of the
	// image's user. Set it for images that would otherwise run as root (e.g. the yolks
	// images): files the game creates then belong to the same uid as the sftp-agent (1000),
	// so the file manager can write them. Install stays as root, and a "fix-owner" step
	// chowns the data volume once per install revision. Leave unset for images whose
	// entrypoint must start as root and drops privileges itself (itzg/*).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	RunAsUser *int64 `json:"runAsUser,omitempty"`
}

// EggStatus defines the observed state of Egg.
type EggStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Egg resource.
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
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.images[0].image`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Egg is the Schema for the eggs API
type Egg struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Egg
	// +required
	Spec EggSpec `json:"spec"`

	// status defines the observed state of Egg
	// +optional
	Status EggStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EggList contains a list of Egg
type EggList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Egg `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Egg{}, &EggList{})
		return nil
	})
}
