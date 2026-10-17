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

// ApplyTarget chooses which members receive CREATE DATABASE and CREATE USER.
type ApplyTarget string

const (
	// ApplyTargetMaster applies statements only on the selected master.
	ApplyTargetMaster ApplyTarget = "Master"
	// ApplyTargetAll applies statements on every cluster member.
	ApplyTargetAll ApplyTarget = "All"
)

// PrivilegeReplication describes whether account and schema DDL replicates.
type PrivilegeReplication string

const (
	// PrivilegeReplicationReplicated means statements are executed on the master and travel via binlog.
	PrivilegeReplicationReplicated PrivilegeReplication = "Replicated"
	// PrivilegeReplicationLocal means each member is updated with sql_log_bin disabled.
	PrivilegeReplicationLocal PrivilegeReplication = "Local"
)

// MasterMode selects how the operator finds the master.
type MasterMode string

const (
	// MasterModeDetected probes members with the configured criteria.
	MasterModeDetected MasterMode = "Detected"
	// MasterModeExplicit uses the member marked master.
	MasterModeExplicit MasterMode = "Explicit"
)

// AuthPlugin is the MySQL authentication plugin for an account.
type AuthPlugin string

const (
	// AuthPluginCachingSHA2 is caching_sha2_password.
	AuthPluginCachingSHA2 AuthPlugin = "caching_sha2_password"
	// AuthPluginNative is mysql_native_password.
	AuthPluginNative AuthPlugin = "mysql_native_password"
)

// PatternType selects how tenant name rules are matched.
type PatternType string

const (
	// PatternGlob matches with * and ?.
	PatternGlob PatternType = "Glob"
	// PatternRegex matches a Go RE2 expression against the whole name.
	PatternRegex PatternType = "Regex"
)

// TLSRequirement is the tenant policy for REQUIRE SSL.
type TLSRequirement string

const (
	// TLSOptional allows accounts with or without REQUIRE SSL.
	TLSOptional TLSRequirement = "Optional"
	// TLSRequired rejects accounts that do not set requireTLS.
	TLSRequired TLSRequirement = "Required"
)

// MySQLMember is one server in a MySQL cluster.
type MySQLMember struct {
	// name is a stable identifier used in status and secret templates.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]+$`
	Name string `json:"name"`

	// host is the DNS name or address the operator dials.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Host string `json:"host"`

	// port is the MySQL port. Defaults to 3306.
	// +kubebuilder:default=3306
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// master marks the explicit primary. It is valid only when spec.master.mode is Explicit.
	// +optional
	Master bool `json:"master,omitempty"`
}

// EndpointStatus is a member address recorded on a database or user.
type EndpointStatus struct {
	// name is the member name.
	Name string `json:"name"`

	// host is the member host.
	// +optional
	Host string `json:"host,omitempty"`

	// port is the member port. 3306 when the cluster omits it.
	// +optional
	Port int32 `json:"port,omitempty"`
}

// AddressPort returns the configured port or 3306.
func (m MySQLMember) AddressPort() int32 {
	if m.Port == 0 {
		return 3306
	}
	return m.Port
}

// CredentialsSecretRef points at a Secret in the same namespace as the MySQLCluster.
type CredentialsSecretRef struct {
	// name is the Secret in the same namespace as the MySQLCluster.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ObjectKeyRef selects one key in a namespaced object next to the MySQLCluster.
type ObjectKeyRef struct {
	// name is the object in the same namespace as the MySQLCluster.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// key is the data key. Defaults to ca.crt.
	// +kubebuilder:default=ca.crt
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Key string `json:"key,omitempty"`
}

// KeyOrDefault returns key, or fallback when key is empty.
func (r *ObjectKeyRef) KeyOrDefault(fallback string) string {
	if r == nil || r.Key == "" {
		return fallback
	}
	return r.Key
}

// TLSMode controls the operator's own connection to MySQL.
type TLSMode string

const (
	// TLSModeDisabled connects without TLS.
	TLSModeDisabled TLSMode = "Disabled"
	// TLSModeRequired requires TLS and verifies the server certificate.
	TLSModeRequired TLSMode = "Required"
	// TLSModeSkipVerify requires TLS and does not verify the server certificate.
	TLSModeSkipVerify TLSMode = "SkipVerify"
)

// ClusterTLS is the operator's TLS configuration for one MySQL cluster.
type ClusterTLS struct {
	// mode is Disabled, Required, or SkipVerify. Defaults to Disabled.
	// +kubebuilder:validation:Enum=Disabled;Required;SkipVerify
	// +kubebuilder:default=Disabled
	// +optional
	Mode TLSMode `json:"mode,omitempty"`

	// caSecret holds a PEM bundle. key defaults to ca.crt.
	// It is trusted in addition to the system pool.
	// Allowed only when mode is Required. Mutually exclusive with caConfigMap.
	// +optional
	CASecret *ObjectKeyRef `json:"caSecret,omitempty"`

	// caConfigMap holds a PEM bundle in data or binaryData. key defaults to ca.crt.
	// It is trusted in addition to the system pool.
	// Allowed only when mode is Required. Mutually exclusive with caSecret.
	// +optional
	CAConfigMap *ObjectKeyRef `json:"caConfigMap,omitempty"`

	// clientSecret holds tls.crt and tls.key for mutual TLS.
	// Allowed when mode is Required or SkipVerify.
	// +optional
	ClientSecret *CredentialsSecretRef `json:"clientSecret,omitempty"`

	// serverName is the certificate name used for verification and SNI.
	// Empty uses each member host.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// Enabled reports whether the operator must open a TLS session.
func (t *ClusterTLS) Enabled() bool {
	return t != nil && t.Mode != "" && t.Mode != TLSModeDisabled
}

// MasterSelection configures master discovery for a cluster.
type MasterSelection struct {
	// mode is Detected or Explicit.
	// +kubebuilder:validation:Enum=Detected;Explicit
	// +required
	Mode MasterMode `json:"mode"`

	// criteria are AND-ed together. Omitted criteria default to enabled.
	// Used only when mode is Detected.
	// +optional
	Criteria *MasterCriteria `json:"criteria,omitempty"`
}

// MasterCriteria lists the probes that a detected master must pass.
type MasterCriteria struct {
	// readOnlyOff requires @@global.read_only = 0. Defaults to true.
	// +optional
	ReadOnlyOff *bool `json:"readOnlyOff,omitempty"`

	// superReadOnlyOff requires @@global.super_read_only = 0. Defaults to true.
	// +optional
	SuperReadOnlyOff *bool `json:"superReadOnlyOff,omitempty"`

	// noReplicationConfigured requires no replication source. Defaults to true.
	// +optional
	NoReplicationConfigured *bool `json:"noReplicationConfigured,omitempty"`
}

// EffectiveCriteria resolves omitted criteria to their defaults.
func (c *MasterCriteria) EffectiveCriteria() (readOnlyOff, superReadOnlyOff, noReplication bool) {
	if c == nil {
		return true, true, true
	}
	return boolDefault(c.ReadOnlyOff, true), boolDefault(c.SuperReadOnlyOff, true), boolDefault(c.NoReplicationConfigured, true)
}

// AnyEnabled reports whether at least one criterion is on.
func (c *MasterCriteria) AnyEnabled() bool {
	readOnlyOff, superReadOnlyOff, noReplication := c.EffectiveCriteria()
	return readOnlyOff || superReadOnlyOff || noReplication
}

func boolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

// PatternRule matches database or user names.
type PatternRule struct {
	// type is Glob or Regex. Defaults to Glob.
	// +kubebuilder:validation:Enum=Glob;Regex
	// +kubebuilder:default=Glob
	// +optional
	Type PatternType `json:"type,omitempty"`

	// patterns are tested until one matches.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Patterns []string `json:"patterns"`
}

// NormalizedType returns Glob when type is empty.
func (p PatternRule) NormalizedType() PatternType {
	if p.Type == "" {
		return PatternGlob
	}
	return p.Type
}
