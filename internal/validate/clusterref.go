package validate

import (
	"fmt"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

// ClusterBinding is the MySQLCluster a database or user reads.
type ClusterBinding struct {
	Name          string
	Namespace     string
	SameNamespace bool
}

// DeprecatedClusterWarning is returned by the webhook when spec.cluster is used alone.
const DeprecatedClusterWarning = "spec.cluster is deprecated; use spec.clusterRef"

// ClusterWarnings returns a warning when the object uses only spec.cluster.
func ClusterWarnings(cluster string, ref *mysqlv1alpha1.ClusterRef) []string {
	if cluster != "" && ref == nil {
		return []string{DeprecatedClusterWarning}
	}
	return nil
}

// ResolveCluster returns the cluster name and namespace for one object.
// spec.cluster alone means the operator namespace. SameNamespace looks only in the object namespace.
// There is no fallback. Exactly one of cluster and clusterRef must be set.
func ResolveCluster(objectNamespace, operatorNamespace, cluster string, ref *mysqlv1alpha1.ClusterRef) (ClusterBinding, error) {
	hasCluster := cluster != ""
	hasRef := ref != nil
	if hasCluster == hasRef {
		return ClusterBinding{}, fmt.Errorf("set exactly one of cluster and clusterRef")
	}
	if hasCluster {
		return ClusterBinding{Name: cluster, Namespace: operatorNamespace}, nil
	}
	if ref.Name == "" {
		return ClusterBinding{}, fmt.Errorf("clusterRef.name is required")
	}
	switch namespaceFrom(ref) {
	case mysqlv1alpha1.NamespaceFromOperator:
		return ClusterBinding{Name: ref.Name, Namespace: operatorNamespace}, nil
	case mysqlv1alpha1.NamespaceFromSame:
		if objectNamespace == "" {
			return ClusterBinding{}, fmt.Errorf("object namespace is required for SameNamespace")
		}
		return ClusterBinding{Name: ref.Name, Namespace: objectNamespace, SameNamespace: true}, nil
	default:
		return ClusterBinding{}, fmt.Errorf("unsupported namespaceFrom %q", ref.NamespaceFrom)
	}
}

// CountsTowardQuota reports whether the object consumes a MySQLTenant quota.
// SameNamespace references do not. Any other reference does, including an invalid one that is not SameNamespace.
func CountsTowardQuota(ref *mysqlv1alpha1.ClusterRef) bool {
	return ref == nil || namespaceFrom(ref) != mysqlv1alpha1.NamespaceFromSame
}

func namespaceFrom(ref *mysqlv1alpha1.ClusterRef) mysqlv1alpha1.NamespaceFrom {
	if ref == nil || ref.NamespaceFrom == "" {
		return mysqlv1alpha1.NamespaceFromOperator
	}
	return ref.NamespaceFrom
}
