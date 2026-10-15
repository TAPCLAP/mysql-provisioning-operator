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
)

// TenantQuotas limits how many objects a namespace may create.
type TenantQuotas struct {
	// maxDatabases is the maximum number of MySQLDatabase objects in each subject namespace.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxDatabases *int32 `json:"maxDatabases,omitempty"`

	// maxUsers is the maximum number of MySQLUser objects in each subject namespace.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxUsers *int32 `json:"maxUsers,omitempty"`
}

// TenantAccess grants one namespace set a slice of one cluster.
type TenantAccess struct {
	// cluster is the MySQLCluster name in the operator namespace.
	// +kubebuilder:validation:MinLength=1
	Cluster string `json:"cluster"`

	// databases names this access entry may create or grant.
	// +required
	Databases PatternRule `json:"databases"`

	// users names this access entry may create.
	// +required
	Users PatternRule `json:"users"`

	// hosts are the exact MySQL account hosts a user may request.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	Hosts []string `json:"hosts,omitempty"`

	// targets are the apply targets a database or user may request.
	// +optional
	// +kubebuilder:validation:MaxItems=2
	Targets []ApplyTarget `json:"targets,omitempty"`

	// requireTLS rejects users that do not set spec.requireTLS when Required.
	// +kubebuilder:validation:Enum=Required;Optional
	// +kubebuilder:default=Optional
	// +optional
	RequireTLS TLSRequirement `json:"requireTLS,omitempty"`

	// privileges are the only privileges a user may grant.
	// +optional
	Privileges []string `json:"privileges,omitempty"`
}

// MySQLTenantSpec binds namespaces to clusters and name patterns.
type MySQLTenantSpec struct {
	// namespaces are the only namespaces that may use this policy.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	Namespaces []string `json:"namespaces"`

	// quotas limit object counts. An omitted quota is unlimited.
	// +optional
	Quotas *TenantQuotas `json:"quotas,omitempty"`

	// access lists the clusters and name patterns this tenant may use.
	// +kubebuilder:validation:MinItems=1
	Access []TenantAccess `json:"access"`
}

// MySQLTenantStatus reports databases that match the policy but have no MySQLDatabase.
type MySQLTenantStatus struct {
	// orphanDatabases are schema names that match a pattern and have no MySQLDatabase.
	// The operator never drops them.
	// +optional
	OrphanDatabases []string `json:"orphanDatabases,omitempty"`

	// conditions represent the current state of the MySQLTenant resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mtenant
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// MySQLTenant is the Schema for the mysqltenants API.
type MySQLTenant struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of MySQLTenant
	// +required
	Spec MySQLTenantSpec `json:"spec"`

	// status defines the observed state of MySQLTenant
	// +optional
	Status MySQLTenantStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MySQLTenantList contains a list of MySQLTenant.
type MySQLTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MySQLTenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MySQLTenant{}, &MySQLTenantList{})
}
