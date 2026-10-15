package controller

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/metrics"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

// MySQLDatabaseReconciler reconciles a MySQLDatabase object.
type MySQLDatabaseReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          events.EventRecorder
	APIReader         client.Reader
	Opener            mysql.Opener
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqldatabases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqldatabases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqldatabases/finalizers,verbs=update
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqltenants,verbs=get;list;watch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlusers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile creates a schema and never drops it.
func (r *MySQLDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	var database mysqlv1alpha1.MySQLDatabase
	if err := r.Get(ctx, req.NamespacedName, &database); err != nil {
		return ctrl.Result{}, ignoreNotFound(err)
	}
	if !database.DeletionTimestamp.IsZero() {
		r.event(&database, corev1.EventTypeNormal, "DatabaseRetained", "MySQL database was retained and was not dropped")
		return ctrl.Result{}, nil
	}
	original := database.DeepCopy()
	if err := validate.Database(ctx, r.Client, r.OperatorNamespace, &database, false); err != nil {
		log.Error(err, "database is not authorized")
		metrics.ReconcileErrors.WithLabelValues("mysqldatabase").Inc()
		setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionAuthorized, metav1.ConditionFalse, "Denied", err.Error())
		setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "Denied", err.Error())
		return r.finish(ctx, original, &database)
	}
	setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionAuthorized, metav1.ConditionTrue, "Allowed", "tenant policy allows this database")
	target, err := r.access().resolve(ctx, database.Spec.Cluster, database.Spec.Target)
	if err != nil {
		return r.fail(ctx, original, &database, err)
	}
	applied, drift, err := r.ensure(ctx, &database, target)
	database.Status.AppliedOn = applied
	if len(drift) > 0 {
		setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionCharacterSetDrift, metav1.ConditionTrue, "Drift", "character set differs on "+strings.Join(drift, ","))
	} else {
		setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionCharacterSetDrift, metav1.ConditionFalse, "Matched", "character set matches or the schema was created")
	}
	if err != nil {
		return r.fail(ctx, original, &database, err)
	}
	setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Applied", "schema exists on every target")
	setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionApplied, metav1.ConditionTrue, "Applied", "schema exists on every target")
	setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionTrue, "Applied", "schema exists on every target")
	return r.finish(ctx, original, &database)
}

func (r *MySQLDatabaseReconciler) ensure(ctx context.Context, database *mysqlv1alpha1.MySQLDatabase, target resolved) ([]string, []string, error) {
	charset := database.Spec.CharacterSet
	if charset == "" {
		charset = mysqlv1alpha1.DefaultCharacterSet
	}
	collation := database.Spec.Collation
	if collation == "" {
		collation = mysqlv1alpha1.DefaultCollation
	}
	var drift []string
	applied, err := applyMembers(ctx, r.access().opener(), target, func(ctx context.Context, member mysqlv1alpha1.MySQLMember, db mysql.DB) error {
		state, err := mysql.EnsureDatabase(ctx, db, database.Spec.Name, charset, collation)
		if err != nil {
			return err
		}
		if state.Exists && (normalizeCharset(state.Charset) != normalizeCharset(charset) || !strings.EqualFold(state.Collation, collation)) {
			drift = append(drift, member.Name)
		}
		return nil
	})
	return applied, drift, err
}

func (r *MySQLDatabaseReconciler) fail(ctx context.Context, original, database *mysqlv1alpha1.MySQLDatabase, err error) (ctrl.Result, error) {
	logf.FromContext(ctx).Error(err, "failed to ensure database")
	metrics.ReconcileErrors.WithLabelValues("mysqldatabase").Inc()
	setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionDegraded, metav1.ConditionTrue, "ApplyFailed", err.Error())
	setCondition(&database.Status.Conditions, database.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "ApplyFailed", err.Error())
	return r.finish(ctx, original, database)
}

func (r *MySQLDatabaseReconciler) finish(ctx context.Context, original, database *mysqlv1alpha1.MySQLDatabase) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original.Status, database.Status) {
		return ctrl.Result{RequeueAfter: requeueObject}, nil
	}
	if err := updateStatus(ctx, r.Client, database); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueObject}, nil
}

func (r *MySQLDatabaseReconciler) access() clusterAccess {
	return clusterAccess{Client: r.Client, APIReader: r.APIReader, Opener: r.Opener, OperatorNamespace: r.OperatorNamespace}
}

func (r *MySQLDatabaseReconciler) event(obj runtime.Object, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, eventType, reason, reason, "%s", message)
	}
}

func normalizeCharset(charset string) string {
	if strings.EqualFold(charset, "utf8") || strings.EqualFold(charset, "utf8mb3") {
		return "utf8mb3"
	}
	return strings.ToLower(charset)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MySQLDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mysqlv1alpha1.MySQLDatabase{}).
		Watches(&mysqlv1alpha1.MySQLCluster{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			return mapByCluster(ctx, r.Client, r.OperatorNamespace, obj, true)
		})).
		Watches(&mysqlv1alpha1.MySQLTenant{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			return mapByTenant(ctx, r.Client, obj, true)
		})).
		Watches(&mysqlv1alpha1.MySQLDatabase{}, handler.Funcs{
			DeleteFunc: func(_ context.Context, e event.TypedDeleteEvent[client.Object], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				if database, ok := e.Object.(*mysqlv1alpha1.MySQLDatabase); ok {
					r.event(database, corev1.EventTypeNormal, "DatabaseRetained", "MySQL database was retained and was not dropped")
				}
			},
		}).
		Named("mysqldatabase").
		Complete(r)
}
