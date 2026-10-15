package controller

import (
	"context"
	"fmt"

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

// MySQLClusterReconciler reconciles a MySQLCluster object.
type MySQLClusterReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	APIReader         client.Reader
	Opener            mysql.Opener
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile probes members and publishes the selected master.
func (r *MySQLClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	var cluster mysqlv1alpha1.MySQLCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, ignoreNotFound(err)
	}
	original := cluster.DeepCopy()
	if err := validate.Cluster(&cluster); err != nil {
		setCondition(&cluster.Status.Conditions, cluster.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "Invalid", err.Error())
		return r.finish(ctx, original, &cluster)
	}
	access := r.access()
	auth, err := readSession(ctx, access.reader(), &cluster)
	if err != nil {
		log.Error(err, "failed to read cluster credentials")
		metrics.ReconcileErrors.WithLabelValues("mysqlcluster").Inc()
		setCondition(&cluster.Status.Conditions, cluster.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "AdminSecret", err.Error())
		return r.finish(ctx, original, &cluster)
	}
	probes := mysql.ProbeAll(ctx, access.opener(), &cluster, auth)
	selection := mysql.SelectMaster(&cluster, probes)
	r.writeStatus(&cluster, selection)
	return r.finish(ctx, original, &cluster)
}

func (r *MySQLClusterReconciler) access() clusterAccess {
	return clusterAccess{
		Client:            r.Client,
		APIReader:         r.APIReader,
		Opener:            r.Opener,
		OperatorNamespace: r.OperatorNamespace,
	}
}

func (r *MySQLClusterReconciler) writeStatus(cluster *mysqlv1alpha1.MySQLCluster, selection mysql.Selection) {
	cluster.Status.Members = mysql.MemberStatus(cluster, selection)
	unreachable := 0
	for _, member := range cluster.Status.Members {
		if !member.Reachable {
			unreachable++
		}
	}
	metrics.UnreachableMembers.WithLabelValues(cluster.Name).Set(float64(unreachable))
	if selection.Master == nil {
		cluster.Status.Master = nil
		cond, reason := masterCondition(selection)
		setCondition(&cluster.Status.Conditions, cluster.Generation, cond, metav1.ConditionTrue, reason, "master was not selected")
		setCondition(&cluster.Status.Conditions, cluster.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, reason, "master was not selected")
		return
	}
	master := selection.Master
	cluster.Status.Master = &mysqlv1alpha1.MasterStatus{
		Name:              master.Name,
		Host:              master.Host,
		Port:              master.AddressPort(),
		Reason:            selection.Reason,
		WouldPassCriteria: selection.WouldPass,
	}
	clearMasterProblems(&cluster.Status.Conditions, cluster.Generation)
	message := fmt.Sprintf("master %s selected by %s", master.Name, selection.Reason)
	setCondition(&cluster.Status.Conditions, cluster.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionTrue, "MasterSelected", message)
}

func clearMasterProblems(conditions *[]metav1.Condition, generation int64) {
	for _, cond := range []string{
		mysqlv1alpha1.ConditionNoMaster,
		mysqlv1alpha1.ConditionSplitBrain,
		mysqlv1alpha1.ConditionMasterUnavailable,
	} {
		setCondition(conditions, generation, cond, metav1.ConditionFalse, "MasterSelected", "a single master is selected")
	}
}

func (r *MySQLClusterReconciler) finish(ctx context.Context, original, cluster *mysqlv1alpha1.MySQLCluster) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original.Status, cluster.Status) {
		return ctrl.Result{RequeueAfter: requeueCluster}, nil
	}
	if err := updateStatus(ctx, r.Client, cluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueCluster}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MySQLClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mysqlv1alpha1.MySQLCluster{}).
		Named("mysqlcluster").
		Complete(r)
}
