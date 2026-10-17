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

// MySQLDatabaseSpec defines a schema to create on an existing cluster.
// The operator never drops the schema.
type MySQLDatabaseSpec struct {
	// cluster is the MySQLCluster name in the operator namespace.
	// +kubebuilder:validation:MinLength=1
	Cluster string `json:"cluster"`

	// name is the MySQL schema name. It is immutable.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name"`

	// characterSet is applied only when the schema is created. Defaults to utf8mb3.
	// +kubebuilder:default=utf8mb3
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="characterSet is immutable"
	// +optional
	CharacterSet string `json:"characterSet,omitempty"`

	// collation is applied only when the schema is created. Defaults to utf8mb3_general_ci.
	// +kubebuilder:default=utf8mb3_general_ci
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="collation is immutable"
	// +optional
	Collation string `json:"collation,omitempty"`

	// target overrides the cluster default. Master or All.
	// +kubebuilder:validation:Enum=Master;All
	// +optional
	Target ApplyTarget `json:"target,omitempty"`
}

// MySQLDatabaseStatus reports where the schema exists.
type MySQLDatabaseStatus struct {
	// appliedOn lists member names where the schema was observed.
	// +optional
	AppliedOn []string `json:"appliedOn,omitempty"`

	// members lists cluster members with host and port.
	// Target Master includes every cluster member. Target All includes the members where the schema was applied.
	// +optional
	// +listType=map
	// +listMapKey=name
	Members []EndpointStatus `json:"members,omitempty"`

	// master is the selected primary at the last reconcile.
	// +optional
	Master *EndpointStatus `json:"master,omitempty"`

	// conditions represent the current state of the MySQLDatabase resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mdb
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.cluster`
// +kubebuilder:printcolumn:name="Schema",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Master",type=string,JSONPath=`.status.master.name`
// +kubebuilder:printcolumn:name="Members",type=string,JSONPath=`.status.appliedOn[*]`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// MySQLDatabase is the Schema for the mysqldatabases API.
type MySQLDatabase struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of MySQLDatabase
	// +required
	Spec MySQLDatabaseSpec `json:"spec"`

	// status defines the observed state of MySQLDatabase
	// +optional
	Status MySQLDatabaseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MySQLDatabaseList contains a list of MySQLDatabase.
type MySQLDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MySQLDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MySQLDatabase{}, &MySQLDatabaseList{})
}
