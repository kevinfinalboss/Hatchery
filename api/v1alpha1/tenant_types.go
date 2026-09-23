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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// TenantFinalizer holds a Tenant's deletion until its namespace is empty
	// of GameServers and backups — see TenantReconciler.reconcileDelete.
	TenantFinalizer = "gameservers.hatchery.io/tenant-finalizer"

	// TenantNamespacePrefix is prepended to a Tenant's name to get its
	// namespace. A fixed prefix means a Tenant can never claim kube-system or
	// any other pre-existing namespace by choosing its name.
	TenantNamespacePrefix = "hatchery-"

	// LabelTenant is set on a tenant's Namespace to the Tenant's name. The
	// GameServerController uses it to tell tenant namespaces from ordinary
	// ones.
	LabelTenant = "gameservers.hatchery.io/tenant"

	// CatalogNamespace holds the global Egg catalog shared by every tenant. It
	// is why the Tenant name "catalog" is reserved (see the CEL rule on Tenant).
	CatalogNamespace = "hatchery-catalog"

	// OperatorNamespace is where the operator itself is deployed (the
	// kubebuilder default). The Tenant name "system" is reserved for the same
	// reason as "catalog": it would map onto a namespace that already exists.
	OperatorNamespace = "hatchery-system"
)

// TenantNamespace returns the namespace that belongs to the Tenant called name.
func TenantNamespace(name string) string {
	return TenantNamespacePrefix + name
}

// TenantQuota is the compute and storage budget of one tenant, translated by
// the TenantController into a ResourceQuota.
type TenantQuota struct {
	// CPU is the total CPU requests all of the tenant's pods may add up to.
	CPU resource.Quantity `json:"cpu"`

	// Memory is the total memory requests all of the tenant's pods may add up to.
	Memory resource.Quantity `json:"memory"`

	// Storage is the total PVC storage the tenant may request.
	Storage resource.Quantity `json:"storage"`

	// MaxGameServers caps how many GameServer objects the tenant may have.
	// +kubebuilder:validation:Minimum=0
	MaxGameServers int32 `json:"maxGameServers"`

	// Backups limits what the organization may keep on the platform's own backup storage. Without
	// it the platform destination is unavailable to the organization. It does not apply to a
	// destination the organization brings itself.
	// +optional
	Backups *TenantBackupQuota `json:"backups,omitempty"`

	// ExtraImageRegistries are registries this organization may pull Egg images from on top of the
	// platform's default list (operator/panel --allowed-image-registries). Entries are matched by
	// whole path segments ("docker.io/itzg" allows docker.io/itzg/*). Only the platform admin
	// edits it. Ignored while the platform list is empty (the check is off).
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:items:MaxLength=253
	// +optional
	ExtraImageRegistries []string `json:"extraImageRegistries,omitempty"`
}

// TenantBackupQuota bounds an organization's use of the platform's backup storage.
type TenantBackupQuota struct {
	// MaxPerServer is how many backups one server may have at the same time.
	// +kubebuilder:validation:Minimum=0
	MaxPerServer int32 `json:"maxPerServer"`

	// MaxPerOrg is how many backups the whole organization may have at the same time.
	// +kubebuilder:validation:Minimum=0
	MaxPerOrg int32 `json:"maxPerOrg"`

	// RetentionDays is how long a backup is kept before the operator deletes it.
	// +kubebuilder:validation:Minimum=1
	RetentionDays int32 `json:"retentionDays"`
}

// TenantSpec defines the desired state of Tenant.
type TenantSpec struct {
	// DisplayName is the human-readable organization name.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Quota is the tenant's resource budget.
	Quota TenantQuota `json:"quota"`
}

// TenantPhase summarizes where a Tenant is in its lifecycle.
type TenantPhase string

const (
	TenantPhasePending     TenantPhase = "Pending"
	TenantPhaseActive      TenantPhase = "Active"
	TenantPhaseTerminating TenantPhase = "Terminating"
	TenantPhaseFailed      TenantPhase = "Failed"
)

// TenantStatus defines the observed state of Tenant.
type TenantStatus struct {
	// Phase summarizes the tenant's lifecycle state.
	// +optional
	Phase TenantPhase `json:"phase,omitempty"`

	// Namespace is the namespace provisioned for this tenant.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the Tenant; "Ready" is the
	// one the Panel API reads.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$') && !(self.metadata.name in ['system', 'catalog'])",message="tenant name must be a DNS label of at most 32 characters and cannot be 'system' or 'catalog' (reserved: they map onto the operator and Egg catalog namespaces)"

// Tenant is the Schema for the tenants API: one organization's isolated slice
// of the cluster.
type Tenant struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec TenantSpec `json:"spec"`

	// +optional
	Status TenantStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Tenant{}, &TenantList{})
		return nil
	})
}
