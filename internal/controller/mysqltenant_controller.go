package controller

import (
	"context"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/metrics"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

// MySQLTenantReconciler reconciles a MySQLTenant object.
type MySQLTenantReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	APIReader         client.Reader
	Opener            mysql.Opener
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqltenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqltenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqltenants/finalizers,verbs=update
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqldatabases,verbs=get;list;watch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlusers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile lists schemas that match the tenant and have no MySQLDatabase.
// It never drops them.
func (r *MySQLTenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var tenant mysqlv1alpha1.MySQLTenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, ignoreNotFound(err)
	}
	if tenant.Namespace != r.OperatorNamespace {
		return ctrl.Result{}, nil
	}
	original := tenant.DeepCopy()
	if err := validate.Tenant(&tenant); err != nil {
		setCondition(&tenant.Status.Conditions, tenant.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "Invalid", err.Error())
		return r.finish(ctx, original, &tenant, requeueObject)
	}
	orphans, err := r.orphans(ctx, &tenant)
	if err != nil {
		logf.FromContext(ctx).Error(err, "failed to list orphan databases")
		metrics.ReconcileErrors.WithLabelValues("mysqltenant").Inc()
		setCondition(&tenant.Status.Conditions, tenant.Generation, mysqlv1alpha1.ConditionDegraded, metav1.ConditionTrue, "ListFailed", err.Error())
		setCondition(&tenant.Status.Conditions, tenant.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "ListFailed", err.Error())
		return r.finish(ctx, original, &tenant, requeueObject)
	}
	sort.Strings(orphans)
	tenant.Status.OrphanDatabases = orphans
	r.recordCounts(ctx, &tenant)
	setCondition(&tenant.Status.Conditions, tenant.Generation, mysqlv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Listed", "orphan report refreshed")
	setCondition(&tenant.Status.Conditions, tenant.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionTrue, "Listed", "orphan report refreshed")
	return r.finish(ctx, original, &tenant, 5*requeueObject)
}

func (r *MySQLTenantReconciler) orphans(ctx context.Context, tenant *mysqlv1alpha1.MySQLTenant) ([]string, error) {
	access := r.access()
	seen := map[string]struct{}{}
	var orphans []string
	for _, entry := range tenant.Spec.Access {
		names, err := r.schemas(ctx, access, entry.Cluster)
		if err != nil {
			return tenant.Status.OrphanDatabases, err
		}
		managed, err := r.managed(ctx, tenant, entry.Cluster)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if validate.SchemaDenied(name) {
				continue
			}
			ok, err := validate.Match(entry.Databases, name)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if _, exists := managed[name]; exists {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			orphans = append(orphans, name)
		}
	}
	return orphans, nil
}

func (r *MySQLTenantReconciler) schemas(ctx context.Context, access clusterAccess, clusterName string) ([]string, error) {
	var cluster mysqlv1alpha1.MySQLCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.OperatorNamespace, Name: clusterName}, &cluster); err != nil {
		return nil, err
	}
	auth, err := readSession(ctx, access.reader(), &cluster)
	if err != nil {
		return nil, err
	}
	selection := mysql.SelectMaster(&cluster, mysql.ProbeAll(ctx, access.opener(), &cluster, auth))
	member := selection.Master
	if member == nil && len(cluster.Spec.Members) > 0 {
		first := cluster.Spec.Members[0]
		member = &first
	}
	if member == nil {
		return nil, masterError(selection)
	}
	db, err := access.opener().Open(ctx, *member, auth)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	return mysql.ListSchemas(ctx, db)
}

func (r *MySQLTenantReconciler) managed(ctx context.Context, tenant *mysqlv1alpha1.MySQLTenant, clusterName string) (map[string]struct{}, error) {
	managed := map[string]struct{}{}
	for _, namespace := range tenant.Spec.Namespaces {
		var list mysqlv1alpha1.MySQLDatabaseList
		if err := r.List(ctx, &list, client.InNamespace(namespace)); err != nil {
			return nil, err
		}
		for _, item := range list.Items {
			if item.Spec.Cluster == clusterName {
				managed[item.Spec.Name] = struct{}{}
			}
		}
	}
	return managed, nil
}

func (r *MySQLTenantReconciler) recordCounts(ctx context.Context, tenant *mysqlv1alpha1.MySQLTenant) {
	for _, namespace := range tenant.Spec.Namespaces {
		var databases mysqlv1alpha1.MySQLDatabaseList
		if err := r.List(ctx, &databases, client.InNamespace(namespace)); err == nil {
			metrics.TenantObjects.WithLabelValues(tenant.Name, namespace, "database").Set(float64(len(databases.Items)))
		}
		var users mysqlv1alpha1.MySQLUserList
		if err := r.List(ctx, &users, client.InNamespace(namespace)); err == nil {
			metrics.TenantObjects.WithLabelValues(tenant.Name, namespace, "user").Set(float64(len(users.Items)))
		}
	}
}

func (r *MySQLTenantReconciler) finish(ctx context.Context, original, tenant *mysqlv1alpha1.MySQLTenant, after time.Duration) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original.Status, tenant.Status) {
		return ctrl.Result{RequeueAfter: after}, nil
	}
	if err := updateStatus(ctx, r.Client, tenant); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: after}, nil
}

func (r *MySQLTenantReconciler) access() clusterAccess {
	return clusterAccess{Client: r.Client, APIReader: r.APIReader, Opener: r.Opener, OperatorNamespace: r.OperatorNamespace}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MySQLTenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mysqlv1alpha1.MySQLTenant{}).
		Named("mysqltenant").
		Complete(r)
}
