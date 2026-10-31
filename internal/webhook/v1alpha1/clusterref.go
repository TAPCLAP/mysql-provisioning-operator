package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

func clusterWarnings(cluster string, ref *mysqlv1alpha1.ClusterRef) admission.Warnings {
	return validate.ClusterWarnings(cluster, ref)
}
