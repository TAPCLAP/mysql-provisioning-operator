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

// MySQLClusterSpec defines one existing MySQL cluster the operator can provision.
// +kubebuilder:validation:XValidation:rule="!(self.privilegeReplication == 'Replicated' && self.defaultTarget == 'All')",message="target All is incompatible with privilegeReplication Replicated"
// +kubebuilder:validation:XValidation:rule="self.master.mode != 'Explicit' || self.members.filter(m, has(m.master) && m.master).size() == 1",message="Explicit mode requires exactly one member with master true"
// +kubebuilder:validation:XValidation:rule="self.master.mode != 'Detected' || !self.members.exists(m, has(m.master) && m.master)",message="Detected mode forbids member.master"
// +kubebuilder:validation:XValidation:rule="!has(self.master.criteria) || (!has(self.master.criteria.readOnlyOff) || self.master.criteria.readOnlyOff) || (!has(self.master.criteria.superReadOnlyOff) || self.master.criteria.superReadOnlyOff) || (!has(self.master.criteria.noReplicationConfigured) || self.master.criteria.noReplicationConfigured)",message="at least one master criterion must be enabled"
type MySQLClusterSpec struct {
	// members are the MySQL servers in this cluster.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Members []MySQLMember `json:"members"`

	// credentialsSecret holds username and password keys for the admin account.
	// +required
	CredentialsSecret CredentialsSecretRef `json:"credentialsSecret"`

	// tls configures the operator connection to MySQL. Omitted or Disabled is plaintext.
	// +optional
	TLS *ClusterTLS `json:"tls,omitempty"`

	// privilegeReplication tells the operator whether DDL replicates through the binlog.
	// +kubebuilder:validation:Enum=Replicated;Local
	// +kubebuilder:default=Replicated
	// +optional
	PrivilegeReplication PrivilegeReplication `json:"privilegeReplication,omitempty"`

	// defaultTarget is used when a database or user omits spec.target.
	// +kubebuilder:validation:Enum=Master;All
	// +kubebuilder:default=Master
	// +optional
	DefaultTarget ApplyTarget `json:"defaultTarget,omitempty"`

	// master configures how the writable primary is chosen.
	// +required
	Master MasterSelection `json:"master"`
}

// MasterStatus is the member currently selected as master.
type MasterStatus struct {
	// name is the member name.
	// +optional
	Name string `json:"name,omitempty"`
	// host is the member host.
	// +optional
	Host string `json:"host,omitempty"`
	// port is the member port.
	// +optional
	Port int32 `json:"port,omitempty"`
	// reason is Detected or Explicit.
	// +optional
	Reason string `json:"reason,omitempty"`
	// wouldPassCriteria reports whether an explicit master also matches criteria.
	// +optional
	WouldPassCriteria *bool `json:"wouldPassCriteria,omitempty"`
}

// MemberStatus is the last probe of one member.
type MemberStatus struct {
	// name is the member name.
	Name string `json:"name"`
	// reachable is true when the probe connected.
	Reachable bool `json:"reachable"`
	// serverID is @@global.server_id.
	// +optional
	ServerID string `json:"serverID,omitempty"`
	// readOnly is @@global.read_only.
	// +optional
	ReadOnly *bool `json:"readOnly,omitempty"`
	// superReadOnly is @@global.super_read_only.
	// +optional
	SuperReadOnly *bool `json:"superReadOnly,omitempty"`
	// replicationConfigured is true when a replication source is configured.
	// +optional
	ReplicationConfigured *bool `json:"replicationConfigured,omitempty"`
	// matchesCriteria is true when every enabled criterion passed.
	MatchesCriteria bool `json:"matchesCriteria"`
	// message explains a probe failure.
	// +optional
	Message string `json:"message,omitempty"`
}

// MySQLClusterStatus is the observed master and member health.
type MySQLClusterStatus struct {
	// master is the selected primary, when one could be chosen.
	// +optional
	Master *MasterStatus `json:"master,omitempty"`

	// members contains one entry per spec member.
	// +optional
	// +listType=map
	// +listMapKey=name
	Members []MemberStatus `json:"members,omitempty"`

	// conditions represent the current state of the MySQLCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mcluster
// +kubebuilder:printcolumn:name="Master",type=string,JSONPath=`.status.master.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MySQLCluster is the Schema for the mysqlclusters API.
type MySQLCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of MySQLCluster
	// +required
	Spec MySQLClusterSpec `json:"spec"`

	// status defines the observed state of MySQLCluster
	// +optional
	Status MySQLClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MySQLClusterList contains a list of MySQLCluster.
type MySQLClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MySQLCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MySQLCluster{}, &MySQLClusterList{})
}
