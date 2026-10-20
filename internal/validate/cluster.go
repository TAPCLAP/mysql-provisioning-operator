package validate

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

const (
	maxAttributes           = 16
	maxAttributeValueLength = 253
)

var (
	serverNamePattern   = regexp.MustCompile(`^[A-Za-z0-9.:_-]+$`)
	attributeKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)
)

// Cluster checks structural rules that do not need other objects.
func Cluster(obj *mysqlv1alpha1.MySQLCluster) error {
	if obj.Spec.CredentialsSecret.Name == "" {
		return fmt.Errorf("credentialsSecret.name is required")
	}
	if len(obj.Spec.Members) == 0 {
		return fmt.Errorf("at least one member is required")
	}
	names := map[string]struct{}{}
	masters := 0
	for _, member := range obj.Spec.Members {
		if _, ok := names[member.Name]; ok {
			return fmt.Errorf("duplicate member name %q", member.Name)
		}
		names[member.Name] = struct{}{}
		if member.Host == "" {
			return fmt.Errorf("member %q host is required", member.Name)
		}
		if err := checkAttributes(member); err != nil {
			return err
		}
		if member.Master {
			masters++
		}
	}
	switch obj.Spec.Master.Mode {
	case mysqlv1alpha1.MasterModeExplicit:
		if masters != 1 {
			return fmt.Errorf("explicit mode requires exactly one member with master: true")
		}
	case mysqlv1alpha1.MasterModeDetected:
		if masters != 0 {
			return fmt.Errorf("detected mode forbids member.master")
		}
	default:
		return fmt.Errorf("master.mode must be Detected or Explicit")
	}
	if !obj.Spec.Master.Criteria.AnyEnabled() {
		return fmt.Errorf("at least one master criterion must be enabled")
	}
	replication := obj.Spec.PrivilegeReplication
	if replication == "" {
		replication = mysqlv1alpha1.PrivilegeReplicationReplicated
	}
	target := obj.Spec.DefaultTarget
	if target == "" {
		target = mysqlv1alpha1.ApplyTargetMaster
	}
	if replication == mysqlv1alpha1.PrivilegeReplicationReplicated && target == mysqlv1alpha1.ApplyTargetAll {
		return fmt.Errorf("defaultTarget All is incompatible with privilegeReplication Replicated")
	}
	return checkClusterTLS(obj.Spec.TLS)
}

func checkAttributes(member mysqlv1alpha1.MySQLMember) error {
	if len(member.Attributes) > maxAttributes {
		return fmt.Errorf("member %q has more than %d attributes", member.Name, maxAttributes)
	}
	for key, value := range member.Attributes {
		if !attributeKeyPattern.MatchString(key) {
			return fmt.Errorf("member %q attribute key %q is invalid", member.Name, key)
		}
		if utf8.RuneCountInString(value) > maxAttributeValueLength {
			return fmt.Errorf("member %q attribute %q exceeds %d characters", member.Name, key, maxAttributeValueLength)
		}
	}
	return nil
}

func checkClusterTLS(tls *mysqlv1alpha1.ClusterTLS) error {
	if tls == nil || !tls.Enabled() {
		if tls != nil && (tls.CASecret != nil || tls.CAConfigMap != nil || tls.ClientSecret != nil || tls.ServerName != "") {
			return fmt.Errorf("tls caSecret, caConfigMap, clientSecret, and serverName require mode Required or SkipVerify")
		}
		return nil
	}
	if tls.Mode != mysqlv1alpha1.TLSModeRequired && tls.Mode != mysqlv1alpha1.TLSModeSkipVerify {
		return fmt.Errorf("tls.mode must be Disabled, Required, or SkipVerify")
	}
	if tls.CASecret != nil && tls.CAConfigMap != nil {
		return fmt.Errorf("tls.caSecret and tls.caConfigMap are mutually exclusive")
	}
	if err := checkCARef("tls.caSecret", tls.Mode, tls.CASecret); err != nil {
		return err
	}
	if err := checkCARef("tls.caConfigMap", tls.Mode, tls.CAConfigMap); err != nil {
		return err
	}
	if tls.ClientSecret != nil && tls.ClientSecret.Name == "" {
		return fmt.Errorf("tls.clientSecret.name is required")
	}
	if tls.ServerName != "" && !serverNamePattern.MatchString(tls.ServerName) {
		return fmt.Errorf("tls.serverName is invalid")
	}
	return nil
}

func checkCARef(field string, mode mysqlv1alpha1.TLSMode, ref *mysqlv1alpha1.ObjectKeyRef) error {
	if ref == nil {
		return nil
	}
	if mode != mysqlv1alpha1.TLSModeRequired {
		return fmt.Errorf("%s is only used when mode is Required", field)
	}
	if ref.Name == "" {
		return fmt.Errorf("%s.name is required", field)
	}
	if ref.Key != "" && !serverNamePattern.MatchString(ref.Key) {
		return fmt.Errorf("%s.key is invalid", field)
	}
	return nil
}

// Tenant checks patterns and the global privilege denylist.
func Tenant(obj *mysqlv1alpha1.MySQLTenant) error {
	if len(obj.Spec.Namespaces) == 0 {
		return fmt.Errorf("at least one namespace is required")
	}
	if len(obj.Spec.Access) == 0 {
		return fmt.Errorf("at least one access entry is required")
	}
	for i, access := range obj.Spec.Access {
		if access.Cluster == "" {
			return fmt.Errorf("access[%d].cluster is required", i)
		}
		if err := checkPatterns(access.Databases); err != nil {
			return fmt.Errorf("access[%d].databases: %w", i, err)
		}
		if err := checkPatterns(access.Users); err != nil {
			return fmt.Errorf("access[%d].users: %w", i, err)
		}
		for _, host := range access.Hosts {
			if !validHost(host) {
				return fmt.Errorf("access[%d] has invalid host %q", i, host)
			}
		}
		for _, target := range access.Targets {
			if target != mysqlv1alpha1.ApplyTargetAll && target != mysqlv1alpha1.ApplyTargetMaster {
				return fmt.Errorf("access[%d] has invalid target %q", i, target)
			}
		}
		for _, priv := range access.Privileges {
			if PrivilegeDenied(priv) {
				return fmt.Errorf("access[%d] privilege %q is forbidden", i, priv)
			}
		}
	}
	return nil
}

func checkPatterns(rule mysqlv1alpha1.PatternRule) error {
	if len(rule.Patterns) == 0 {
		return fmt.Errorf("at least one pattern is required")
	}
	for _, pattern := range rule.Patterns {
		if _, err := Match(mysqlv1alpha1.PatternRule{Type: rule.NormalizedType(), Patterns: []string{pattern}}, "placeholder"); err != nil {
			return err
		}
	}
	return nil
}
