package validate

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

var (
	identPattern   = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)
	hostPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:%-]+$`)
	charsetPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

// Database checks tenant policy before a schema is created.
func Database(ctx context.Context, c client.Client, operatorNamespace string, obj *mysqlv1alpha1.MySQLDatabase, creating bool) error {
	if !validIdent(obj.Spec.Name, 64) {
		return fmt.Errorf("invalid database name %q", obj.Spec.Name)
	}
	if SchemaDenied(obj.Spec.Name) {
		return fmt.Errorf("database %q is forbidden", obj.Spec.Name)
	}
	charset := obj.Spec.CharacterSet
	if charset == "" {
		charset = mysqlv1alpha1.DefaultCharacterSet
	}
	collation := obj.Spec.Collation
	if collation == "" {
		collation = mysqlv1alpha1.DefaultCollation
	}
	if !charsetPattern.MatchString(charset) || !charsetPattern.MatchString(collation) {
		return fmt.Errorf("invalid character set or collation")
	}
	cluster, err := loadCluster(ctx, c, operatorNamespace, obj.Spec.Cluster)
	if err != nil {
		return err
	}
	if err := Cluster(cluster); err != nil {
		return err
	}
	target := EffectiveTarget(obj.Spec.Target, cluster)
	if err := checkReplicationTarget(cluster, target); err != nil {
		return err
	}
	tenants, err := loadTenants(ctx, c, operatorNamespace)
	if err != nil {
		return err
	}
	if err := authorizeDatabase(tenants, obj.Namespace, obj.Spec.Cluster, obj.Spec.Name, target); err != nil {
		return err
	}
	return checkQuota(ctx, c, tenants, obj.Namespace, obj.Spec.Cluster, true, creating)
}

// User checks tenant policy before an account is created or changed.
func User(ctx context.Context, c client.Client, operatorNamespace string, obj *mysqlv1alpha1.MySQLUser, creating bool) error {
	if !validIdent(obj.Spec.Username, 32) {
		return fmt.Errorf("invalid username %q", obj.Spec.Username)
	}
	suffix, err := UsernameSuffixLength(obj.Spec.UsernameSuffix)
	if err != nil {
		return err
	}
	if suffix > 0 && len(obj.Spec.Username)+suffix > 32 {
		return fmt.Errorf("username plus usernameSuffix.length must be at most 32 characters")
	}
	if obj.Spec.Secret.Name == "" {
		return fmt.Errorf("secret.name is required")
	}
	if obj.Spec.AuthPlugin != mysqlv1alpha1.AuthPluginCachingSHA2 && obj.Spec.AuthPlugin != mysqlv1alpha1.AuthPluginNative {
		return fmt.Errorf("unsupported auth plugin %q", obj.Spec.AuthPlugin)
	}
	hosts := EffectiveHosts(obj.Spec.Hosts)
	for _, host := range hosts {
		if !validHost(host) {
			return fmt.Errorf("invalid host %q", host)
		}
	}
	retention, err := ParseRetention(obj.Spec.Password.Retention)
	if err != nil {
		return err
	}
	if _, err := ParseRotateEvery(obj.Spec.Password.RotateEvery, retention); err != nil {
		return err
	}
	for _, grant := range obj.Spec.Grants {
		if !validIdent(grant.Database, 64) || SchemaDenied(grant.Database) {
			return fmt.Errorf("invalid grant database %q", grant.Database)
		}
		for _, table := range grant.Tables {
			if table != "*" && !validIdent(table, 64) {
				return fmt.Errorf("invalid table %q", table)
			}
		}
		for _, priv := range grant.Privileges {
			if PrivilegeDenied(priv) {
				return fmt.Errorf("privilege %q is forbidden", priv)
			}
		}
	}
	cluster, err := loadCluster(ctx, c, operatorNamespace, obj.Spec.Cluster)
	if err != nil {
		return err
	}
	if err := Cluster(cluster); err != nil {
		return err
	}
	target := EffectiveTarget(obj.Spec.Target, cluster)
	if err := checkReplicationTarget(cluster, target); err != nil {
		return err
	}
	tenants, err := loadTenants(ctx, c, operatorNamespace)
	if err != nil {
		return err
	}
	if err := authorizeUser(tenants, obj, hosts, target); err != nil {
		return err
	}
	return checkQuota(ctx, c, tenants, obj.Namespace, obj.Spec.Cluster, false, creating)
}

// EffectiveHosts defaults an empty host list to %.
func EffectiveHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return []string{"%"}
	}
	return hosts
}

// EffectiveTarget uses the cluster default when the object omits target.
func EffectiveTarget(target mysqlv1alpha1.ApplyTarget, cluster *mysqlv1alpha1.MySQLCluster) mysqlv1alpha1.ApplyTarget {
	if target != "" {
		return target
	}
	if cluster.Spec.DefaultTarget != "" {
		return cluster.Spec.DefaultTarget
	}
	return mysqlv1alpha1.ApplyTargetMaster
}

func checkReplicationTarget(cluster *mysqlv1alpha1.MySQLCluster, target mysqlv1alpha1.ApplyTarget) error {
	replication := cluster.Spec.PrivilegeReplication
	if replication == "" {
		replication = mysqlv1alpha1.PrivilegeReplicationReplicated
	}
	if replication == mysqlv1alpha1.PrivilegeReplicationReplicated && target == mysqlv1alpha1.ApplyTargetAll {
		return fmt.Errorf("target All is incompatible with privilegeReplication Replicated")
	}
	return nil
}

func loadCluster(ctx context.Context, c client.Client, operatorNamespace, name string) (*mysqlv1alpha1.MySQLCluster, error) {
	var cluster mysqlv1alpha1.MySQLCluster
	if err := c.Get(ctx, types.NamespacedName{Namespace: operatorNamespace, Name: name}, &cluster); err != nil {
		return nil, fmt.Errorf("get cluster %s/%s: %w", operatorNamespace, name, err)
	}
	return &cluster, nil
}

func loadTenants(ctx context.Context, c client.Client, operatorNamespace string) ([]mysqlv1alpha1.MySQLTenant, error) {
	var list mysqlv1alpha1.MySQLTenantList
	if err := c.List(ctx, &list, client.InNamespace(operatorNamespace)); err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	return list.Items, nil
}

func authorizeDatabase(tenants []mysqlv1alpha1.MySQLTenant, namespace, cluster, name string, target mysqlv1alpha1.ApplyTarget) error {
	var targets []mysqlv1alpha1.ApplyTarget
	matched := false
	for _, tenant := range tenants {
		if !contains(tenant.Spec.Namespaces, namespace) {
			continue
		}
		for _, access := range tenant.Spec.Access {
			if access.Cluster != cluster {
				continue
			}
			ok, err := Match(access.Databases, name)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			matched = true
			targets = append(targets, access.Targets...)
		}
	}
	if !matched {
		return fmt.Errorf("namespace %s cannot create database %s on cluster %s", namespace, name, cluster)
	}
	if !targetAllowed(targets, target) {
		return fmt.Errorf("target %s is not allowed for database %s", target, name)
	}
	return nil
}

func authorizeUser(tenants []mysqlv1alpha1.MySQLTenant, obj *mysqlv1alpha1.MySQLUser, hosts []string, target mysqlv1alpha1.ApplyTarget) error {
	var matched []mysqlv1alpha1.TenantAccess
	for _, tenant := range tenants {
		if !contains(tenant.Spec.Namespaces, obj.Namespace) {
			continue
		}
		for _, access := range tenant.Spec.Access {
			if access.Cluster != obj.Spec.Cluster {
				continue
			}
			ok, err := Match(access.Users, obj.Spec.Username)
			if err != nil {
				return err
			}
			if ok {
				matched = append(matched, access)
			}
		}
	}
	if len(matched) == 0 {
		return fmt.Errorf("namespace %s cannot create user %s on cluster %s", obj.Namespace, obj.Spec.Username, obj.Spec.Cluster)
	}
	var targets []mysqlv1alpha1.ApplyTarget
	hostsAllowed := map[string]struct{}{}
	requireTLS := false
	for _, access := range matched {
		targets = append(targets, access.Targets...)
		for _, host := range access.Hosts {
			hostsAllowed[host] = struct{}{}
		}
		if access.RequireTLS == mysqlv1alpha1.TLSRequired {
			requireTLS = true
		}
	}
	if !targetAllowed(targets, target) {
		return fmt.Errorf("target %s is not allowed for user %s", target, obj.Spec.Username)
	}
	for _, host := range hosts {
		if _, ok := hostsAllowed[host]; !ok {
			return fmt.Errorf("host %q is not allowed for user %s", host, obj.Spec.Username)
		}
	}
	if requireTLS && !obj.Spec.RequireTLS {
		return fmt.Errorf("tenant policy requires REQUIRE SSL")
	}
	for _, grant := range obj.Spec.Grants {
		if err := authorizeGrant(matched, grant); err != nil {
			return err
		}
	}
	return nil
}

func authorizeGrant(matched []mysqlv1alpha1.TenantAccess, grant mysqlv1alpha1.GrantSpec) error {
	for _, priv := range grant.Privileges {
		found := false
		for _, access := range matched {
			ok, err := Match(access.Databases, grant.Database)
			if err != nil {
				return err
			}
			if !ok || !privilegeAllowed(access.Privileges, priv) {
				continue
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("privilege %s on %s is not allowed", priv, grant.Database)
		}
	}
	return nil
}

func privilegeAllowed(allowed []string, priv string) bool {
	want := NormalizePrivilege(priv)
	for _, item := range allowed {
		if NormalizePrivilege(item) == want {
			return true
		}
	}
	return false
}

func targetAllowed(allowed []mysqlv1alpha1.ApplyTarget, target mysqlv1alpha1.ApplyTarget) bool {
	return slices.Contains(allowed, target)
}

func checkQuota(ctx context.Context, c client.Client, tenants []mysqlv1alpha1.MySQLTenant, namespace, cluster string, database bool, creating bool) error {
	unlimited := false
	var max *int32
	for _, tenant := range tenants {
		if !contains(tenant.Spec.Namespaces, namespace) || !tenantAllowsCluster(tenant, cluster) {
			continue
		}
		var ptr *int32
		if tenant.Spec.Quotas != nil {
			if database {
				ptr = tenant.Spec.Quotas.MaxDatabases
			} else {
				ptr = tenant.Spec.Quotas.MaxUsers
			}
		}
		if ptr == nil {
			unlimited = true
			continue
		}
		if max == nil || *ptr > *max {
			value := *ptr
			max = &value
		}
	}
	if unlimited || max == nil {
		return nil
	}
	count, err := countObjects(ctx, c, namespace, database)
	if err != nil {
		return err
	}
	if creating && count >= int(*max) {
		return fmt.Errorf("quota exceeded")
	}
	if !creating && count > int(*max) {
		return fmt.Errorf("quota exceeded")
	}
	return nil
}

func countObjects(ctx context.Context, c client.Client, namespace string, database bool) (int, error) {
	if database {
		var list mysqlv1alpha1.MySQLDatabaseList
		if err := c.List(ctx, &list, client.InNamespace(namespace)); err != nil {
			return 0, err
		}
		return len(list.Items), nil
	}
	var list mysqlv1alpha1.MySQLUserList
	if err := c.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return 0, err
	}
	return len(list.Items), nil
}

func tenantAllowsCluster(tenant mysqlv1alpha1.MySQLTenant, cluster string) bool {
	for _, access := range tenant.Spec.Access {
		if access.Cluster == cluster {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	return slices.Contains(values, want)
}

func validIdent(name string, maxLen int) bool {
	return name != "" && len(name) <= maxLen && identPattern.MatchString(name)
}

func validHost(host string) bool {
	return host != "" && len(host) <= 255 && hostPattern.MatchString(host)
}

// DefaultUsernameSuffixLength is used when usernameSuffix is set and length is omitted.
const DefaultUsernameSuffixLength = 8

// UsernameSuffixLength returns how many random characters to append.
// Zero means the account name is spec.username.
func UsernameSuffixLength(suffix *mysqlv1alpha1.UsernameSuffix) (int, error) {
	if suffix == nil {
		return 0, nil
	}
	n := int(suffix.Length)
	if n == 0 {
		return DefaultUsernameSuffixLength, nil
	}
	if n < 1 || n > 31 {
		return 0, fmt.Errorf("usernameSuffix.length must be from 1 to 31")
	}
	return n, nil
}
