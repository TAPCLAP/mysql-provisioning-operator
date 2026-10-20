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

// PasswordSpec controls how the operator rotates an account password.
// +kubebuilder:validation:XValidation:rule="self.retention.size() == 0 || duration(self.retention) >= duration('5m')",message="retention must be at least 5m"
// +kubebuilder:validation:XValidation:rule="!has(self.rotateEvery) || self.rotateEvery.size() == 0 || duration(self.rotateEvery) > duration(self.retention.size() == 0 ? '24h' : self.retention)",message="rotateEvery must be longer than retention"
type PasswordSpec struct {
	// retention is how long the previous password stays valid. Default 24h, minimum 5m.
	// +kubebuilder:default="24h"
	// +optional
	Retention string `json:"retention,omitempty"`

	// generation is incremented to rotate the password once.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Generation int32 `json:"generation,omitempty"`

	// rotateEvery rotates on a schedule. Empty means only generation bumps rotate.
	// +optional
	RotateEvery string `json:"rotateEvery,omitempty"`
}

// GrantSpec is one set of privileges on a database.
type GrantSpec struct {
	// database is the schema that receives the privileges.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Database string `json:"database"`

	// tables limits the grant. Empty or "*" means every table.
	// +optional
	Tables []string `json:"tables,omitempty"`

	// privileges are the MySQL privileges to grant.
	// +kubebuilder:validation:MinItems=1
	Privileges []string `json:"privileges"`
}

// SecretEntry expands one template for each cluster member.
type SecretEntry struct {
	// forEach is the collection to iterate. Only "members" is supported.
	// Inside the template, .Item.Index is the 1-based position in spec.members.
	// +kubebuilder:validation:Enum=members
	ForEach string `json:"forEach"`

	// key is a Go template that renders the Secret key.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// value is a Go template that renders the Secret value.
	// +optional
	Value string `json:"value"`
}

// SecretTemplate renders the application Secret next to the MySQLUser.
type SecretTemplate struct {
	// name is the Secret name in the same namespace as the MySQLUser.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// data maps Secret keys to Go templates.
	// +optional
	Data map[string]string `json:"data,omitempty"`

	// entries expand additional keys for each cluster member.
	// +optional
	Entries []SecretEntry `json:"entries,omitempty"`
}

// UsernameSuffix appends random characters to spec.username.
type UsernameSuffix struct {
	// length is how many random characters are appended. Default 8.
	// The full account name stays within 32 characters.
	// +kubebuilder:default=8
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=31
	// +optional
	Length int32 `json:"length,omitempty"`
}

// MySQLUserSpec defines an account, its grants, and the Secret that publishes the password.
// +kubebuilder:validation:XValidation:rule="!has(self.usernameSuffix) || size(self.username) + (self.usernameSuffix.length > 0 ? self.usernameSuffix.length : 8) <= 32",message="username plus the random suffix must be at most 32 characters"
type MySQLUserSpec struct {
	// cluster is the MySQLCluster name in the operator namespace.
	// +kubebuilder:validation:MinLength=1
	Cluster string `json:"cluster"`

	// username is the MySQL account name, or the prefix when usernameSuffix is set.
	// It is immutable and at most 32 characters.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="username is immutable"
	Username string `json:"username"`

	// usernameSuffix appends random letters and digits to username.
	// Omit it to use username as the account name.
	// +optional
	UsernameSuffix *UsernameSuffix `json:"usernameSuffix,omitempty"`

	// hosts are the MySQL account hosts. Empty defaults to ["%"].
	// Each user@host pair is a separate account.
	// +optional
	Hosts []string `json:"hosts,omitempty"`

	// authPlugin is caching_sha2_password or mysql_native_password.
	// +kubebuilder:validation:Enum=caching_sha2_password;mysql_native_password
	// +required
	AuthPlugin AuthPlugin `json:"authPlugin"`

	// requireTLS sets REQUIRE SSL when true and REQUIRE NONE when false.
	// +optional
	RequireTLS bool `json:"requireTLS,omitempty"`

	// target overrides the cluster default. Master or All.
	// +kubebuilder:validation:Enum=Master;All
	// +optional
	Target ApplyTarget `json:"target,omitempty"`

	// password configures generation and rotation. The password itself is never stored here.
	// +optional
	Password PasswordSpec `json:"password,omitempty"`

	// grants are reconciled to SHOW GRANTS.
	// +optional
	Grants []GrantSpec `json:"grants,omitempty"`

	// maxUserConnections sets MAX_USER_CONNECTIONS when provided.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxUserConnections *int32 `json:"maxUserConnections,omitempty"`

	// maxQueriesPerHour sets MAX_QUERIES_PER_HOUR when provided.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxQueriesPerHour *int32 `json:"maxQueriesPerHour,omitempty"`

	// secret is the template for the application Secret.
	// +required
	Secret SecretTemplate `json:"secret"`
}

// PasswordStatus records rotation bookkeeping. It never contains the password.
type PasswordStatus struct {
	// generation is the spec.password.generation last applied.
	// +optional
	Generation int32 `json:"generation,omitempty"`

	// hash is the SHA-256 of the password applied to MySQL.
	// +optional
	Hash string `json:"hash,omitempty"`

	// rotatedAt is the time of the last password change.
	// +optional
	RotatedAt *metav1.Time `json:"rotatedAt,omitempty"`

	// retainedUntil is when the previous password is discarded.
	// +optional
	RetainedUntil *metav1.Time `json:"retainedUntil,omitempty"`
}

// MySQLUserStatus reports accounts and password rotation.
type MySQLUserStatus struct {
	// username is the MySQL account name chosen for this object.
	// +optional
	Username string `json:"username,omitempty"`

	// appliedOn lists member names where the account was applied.
	// +optional
	AppliedOn []string `json:"appliedOn,omitempty"`

	// members lists cluster members with host, port, and attributes.
	// Target Master includes every cluster member. Target All includes the members where the account was applied.
	// +optional
	// +listType=map
	// +listMapKey=name
	Members []EndpointStatus `json:"members,omitempty"`

	// memberNames is the comma-separated member names from members.
	// kubectl prints this string. A JSONPath that matches several names stays empty.
	// +optional
	MemberNames string `json:"memberNames,omitempty"`

	// master is the selected primary at the last reconcile.
	// +optional
	Master *EndpointStatus `json:"master,omitempty"`

	// managedHosts are the hosts the operator last created, used to drop removed hosts.
	// +optional
	ManagedHosts []string `json:"managedHosts,omitempty"`

	// password holds rotation bookkeeping.
	// +optional
	Password PasswordStatus `json:"password,omitempty"`

	// conditions represent the current state of the MySQLUser resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=muser
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.cluster`
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.spec.username`
// +kubebuilder:printcolumn:name="Secret",type=string,JSONPath=`.spec.secret.name`
// +kubebuilder:printcolumn:name="Master",type=string,JSONPath=`.status.master.name`
// +kubebuilder:printcolumn:name="Members",type=string,JSONPath=`.status.memberNames`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MySQLUser is the Schema for the mysqlusers API.
type MySQLUser struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of MySQLUser
	// +required
	Spec MySQLUserSpec `json:"spec"`

	// status defines the observed state of MySQLUser
	// +optional
	Status MySQLUserStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MySQLUserList contains a list of MySQLUser.
type MySQLUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MySQLUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MySQLUser{}, &MySQLUserList{})
}
