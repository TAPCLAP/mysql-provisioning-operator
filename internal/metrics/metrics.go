package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileErrors counts failed reconciles by controller.
	ReconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mysql_provisioning_reconcile_errors_total",
		Help: "Number of reconcile errors.",
	}, []string{"controller"})

	// UnreachableMembers is the latest number of members that failed a probe.
	UnreachableMembers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mysql_provisioning_unreachable_members",
		Help: "Members that failed the last probe.",
	}, []string{"cluster"})

	// TenantObjects is the number of databases or users in a tenant namespace.
	TenantObjects = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mysql_provisioning_tenant_objects",
		Help: "Databases and users visible to a tenant.",
	}, []string{"tenant", "namespace", "kind"})

	// RetainedPasswords is the number of users that still accept a previous password.
	RetainedPasswords = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mysql_provisioning_retained_passwords",
		Help: "Users whose previous password is still valid.",
	})
)

func init() {
	metrics.Registry.MustRegister(ReconcileErrors, UnreachableMembers, TenantObjects, RetainedPasswords)
}
