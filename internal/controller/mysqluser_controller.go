package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/metrics"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/password"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

// MySQLUserReconciler reconciles a MySQLUser object.
type MySQLUserReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          events.EventRecorder
	APIReader         client.Reader
	Opener            mysql.Opener
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlusers/finalizers,verbs=update
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqlclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqltenants,verbs=get;list;watch
// +kubebuilder:rbac:groups=mysql-provisioning.tapclap.com,resources=mysqldatabases,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile creates the MySQL account, grants, and application Secret.
func (r *MySQLUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var user mysqlv1alpha1.MySQLUser
	if err := r.Get(ctx, req.NamespacedName, &user); err != nil {
		return ctrl.Result{}, ignoreNotFound(err)
	}
	if !user.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, &user)
	}
	if controllerutil.AddFinalizer(&user, mysqlv1alpha1.UserFinalizer) {
		if err := r.Update(ctx, &user); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return r.reconcile(ctx, &user)
}

func (r *MySQLUserReconciler) reconcile(ctx context.Context, user *mysqlv1alpha1.MySQLUser) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	original := user.DeepCopy()
	if err := validate.User(ctx, r.Client, r.OperatorNamespace, user, false); err != nil {
		log.Error(err, "user is not authorized")
		metrics.ReconcileErrors.WithLabelValues("mysqluser").Inc()
		setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionAuthorized, metav1.ConditionFalse, "Denied", err.Error())
		setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "Denied", err.Error())
		return r.finish(ctx, original, user, requeueObject)
	}
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionAuthorized, metav1.ConditionTrue, "Allowed", "tenant policy allows this user")
	if err := r.ensureAccountName(ctx, user); err != nil {
		return r.fail(ctx, original, user, err)
	}

	target, err := r.access().resolve(ctx, user.Spec.Cluster, user.Spec.Target)
	if err != nil {
		return r.fail(ctx, original, user, err)
	}
	secret, err := r.readAppSecret(ctx, user)
	if err != nil {
		return r.fail(ctx, original, user, err)
	}
	supports, err := r.dualPassword(ctx, target)
	if err != nil {
		return r.fail(ctx, original, user, err)
	}
	plan, err := r.plan(user, secret, supports)
	if err != nil {
		return r.fail(ctx, original, user, err)
	}
	r.noteRotation(user, plan)
	err = r.applyAccounts(ctx, user, target, plan)
	user.Status.Members, user.Status.Master = recordPlacement(target, user.Status.AppliedOn)
	user.Status.MemberNames = memberNameList(user.Status.Members)
	if err != nil {
		setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionGrantsDrift, metav1.ConditionTrue, "ApplyFailed", err.Error())
		return r.fail(ctx, original, user, err)
	}
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionGrantsDrift, metav1.ConditionFalse, "Applied", "grants match the spec")
	if err := r.publishSecret(ctx, user, target, secret, plan); err != nil {
		setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionSecretSynced, metav1.ConditionFalse, "SecretFailed", err.Error())
		return r.fail(ctx, original, user, err)
	}
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionSecretSynced, metav1.ConditionTrue, "Synced", "application secret updated")
	r.storePasswordStatus(user, plan)
	user.Status.ManagedHosts = effectiveHosts(user)
	r.countRetained(ctx)
	ready := !plan.Blocked && !plan.Unsupported
	status := metav1.ConditionTrue
	reason := "Applied"
	if !ready {
		status = metav1.ConditionFalse
		reason = "RotationPending"
	}
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Applied", "account applied on every target")
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionApplied, metav1.ConditionTrue, "Applied", "account applied on every target")
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionReady, status, reason, "account applied on every target")
	return r.finish(ctx, original, user, r.requeueAfter(user, plan))
}

func (r *MySQLUserReconciler) plan(user *mysqlv1alpha1.MySQLUser, secret appSecret, supports bool) (password.Output, error) {
	retention, err := validate.ParseRetention(user.Spec.Password.Retention)
	if err != nil {
		return password.Output{}, err
	}
	every, err := validate.ParseRotateEvery(user.Spec.Password.RotateEvery, retention)
	if err != nil {
		return password.Output{}, err
	}
	retained := secret.retainedUntil
	if user.Status.Password.RetainedUntil != nil {
		retained = user.Status.Password.RetainedUntil.Time
	}
	var rotated time.Time
	if user.Status.Password.RotatedAt != nil {
		rotated = user.Status.Password.RotatedAt.Time
	}
	return password.Plan(password.Input{
		Now:                  time.Now(),
		SpecGeneration:       user.Spec.Password.Generation,
		StatusGeneration:     user.Status.Password.Generation,
		Retention:            retention,
		RotateEvery:          every,
		RotatedAt:            rotated,
		SecretPassword:       secret.password,
		AppliedHash:          user.Status.Password.Hash,
		RetainedUntil:        retained,
		ServerSupportsRetain: supports,
	})
}

func (r *MySQLUserReconciler) noteRotation(user *mysqlv1alpha1.MySQLUser, plan password.Output) {
	rotation := metav1.ConditionFalse
	reason := "Idle"
	message := "password is current"
	if plan.Blocked {
		rotation = metav1.ConditionTrue
		reason = "RetentionOpen"
		message = "previous password is still retained"
	}
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionRotationInProgress, rotation, reason, message)
	unsupported := metav1.ConditionFalse
	ureason := "Supported"
	umessage := "server can retain a password"
	if plan.Unsupported {
		unsupported = metav1.ConditionTrue
		ureason = "Unsupported"
		umessage = "server cannot retain the previous password"
	}
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionPasswordRetentionUnsupported, unsupported, ureason, umessage)
}

func (r *MySQLUserReconciler) applyAccounts(ctx context.Context, user *mysqlv1alpha1.MySQLUser, target resolved, plan password.Output) error {
	hosts := effectiveHosts(user)
	opener := r.access().opener()
	for _, host := range removedHosts(user.Status.ManagedHosts, hosts) {
		if _, err := applyMembers(ctx, opener, target, func(ctx context.Context, _ mysqlv1alpha1.MySQLMember, db mysql.DB) error {
			return mysql.DropAccount(ctx, db, accountName(user), host)
		}); err != nil {
			return err
		}
	}
	var applied []string
	for _, host := range hosts {
		names, err := applyMembers(ctx, opener, target, func(ctx context.Context, _ mysqlv1alpha1.MySQLMember, db mysql.DB) error {
			return mysql.ApplyAccount(ctx, db, accountFor(user, host, plan))
		})
		if err != nil {
			user.Status.AppliedOn = applied
			return err
		}
		applied = names
	}
	user.Status.AppliedOn = applied
	return nil
}

func accountFor(user *mysqlv1alpha1.MySQLUser, host string, plan password.Output) mysql.Account {
	return mysql.Account{
		User:               accountName(user),
		Host:               host,
		Plugin:             user.Spec.AuthPlugin,
		Password:           plan.Password,
		ApplyPassword:      plan.ApplyPassword,
		Retain:             plan.Retain,
		RequireTLS:         user.Spec.RequireTLS,
		DiscardOld:         plan.DiscardOld,
		MaxUserConnections: user.Spec.MaxUserConnections,
		MaxQueriesPerHour:  user.Spec.MaxQueriesPerHour,
		Grants:             user.Spec.Grants,
	}
}

func (r *MySQLUserReconciler) publishSecret(ctx context.Context, user *mysqlv1alpha1.MySQLUser, target resolved, secret appSecret, plan password.Output) error {
	previous := secret.previous
	if plan.Previous != "" {
		previous = plan.Previous
	}
	if plan.DiscardOld || plan.RetainedUntil.IsZero() {
		previous = plan.Previous
	}
	rendered, err := renderSecret(user, target, plan, previous)
	if err != nil {
		return err
	}
	return r.writeAppSecret(ctx, user, rendered, plan.Hash, plan.RetainedUntil)
}

func (r *MySQLUserReconciler) storePasswordStatus(user *mysqlv1alpha1.MySQLUser, plan password.Output) {
	user.Status.Password.Generation = plan.StatusGeneration
	user.Status.Password.Hash = plan.Hash
	if plan.RotatedAt.IsZero() {
		user.Status.Password.RotatedAt = nil
	} else {
		rotated := metav1.NewTime(plan.RotatedAt)
		user.Status.Password.RotatedAt = &rotated
	}
	if plan.RetainedUntil.IsZero() {
		user.Status.Password.RetainedUntil = nil
	} else {
		until := metav1.NewTime(plan.RetainedUntil)
		user.Status.Password.RetainedUntil = &until
	}
}

func (r *MySQLUserReconciler) dualPassword(ctx context.Context, target resolved) (bool, error) {
	supports := true
	known := false
	for _, member := range target.Members {
		db, err := r.access().opener().Open(ctx, member, target.Auth)
		if err != nil {
			continue
		}
		version, err := mysql.ServerVersion(ctx, db)
		_ = db.Close()
		if err != nil {
			return false, err
		}
		known = true
		if !mysql.SupportsDualPassword(version) {
			supports = false
		}
	}
	if !known {
		return false, nil
	}
	return supports, nil
}

func (r *MySQLUserReconciler) finalize(ctx context.Context, user *mysqlv1alpha1.MySQLUser) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(user, mysqlv1alpha1.UserFinalizer) {
		if err := r.dropAll(ctx, user); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err := r.deleteAppSecret(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(user, mysqlv1alpha1.UserFinalizer)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MySQLUserReconciler) dropAll(ctx context.Context, user *mysqlv1alpha1.MySQLUser) error {
	if _, err := r.access().loadCluster(ctx, user.Spec.Cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.event(user, corev1.EventTypeWarning, "ClusterMissing", "cluster is gone; MySQL accounts were not dropped")
			return nil
		}
		return err
	}
	target, err := r.access().resolve(ctx, user.Spec.Cluster, user.Spec.Target)
	if err != nil {
		return err
	}
	hosts := append(effectiveHosts(user), removedHosts(user.Status.ManagedHosts, effectiveHosts(user))...)
	seen := map[string]struct{}{}
	for _, host := range hosts {
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		if _, err := applyMembers(ctx, r.access().opener(), target, func(ctx context.Context, _ mysqlv1alpha1.MySQLMember, db mysql.DB) error {
			return mysql.DropAccount(ctx, db, accountName(user), host)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *MySQLUserReconciler) fail(ctx context.Context, original, user *mysqlv1alpha1.MySQLUser, err error) (ctrl.Result, error) {
	logf.FromContext(ctx).Error(err, "failed to reconcile user")
	metrics.ReconcileErrors.WithLabelValues("mysqluser").Inc()
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionDegraded, metav1.ConditionTrue, "ApplyFailed", err.Error())
	setCondition(&user.Status.Conditions, user.Generation, mysqlv1alpha1.ConditionReady, metav1.ConditionFalse, "ApplyFailed", err.Error())
	return r.finish(ctx, original, user, requeueObject)
}

func (r *MySQLUserReconciler) finish(ctx context.Context, original, user *mysqlv1alpha1.MySQLUser, after time.Duration) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original.Status, user.Status) {
		return ctrl.Result{RequeueAfter: after}, nil
	}
	if err := updateStatus(ctx, r.Client, user); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: after}, nil
}

func (r *MySQLUserReconciler) requeueAfter(user *mysqlv1alpha1.MySQLUser, plan password.Output) time.Duration {
	after := requeueObject
	if !plan.RetainedUntil.IsZero() {
		after = sooner(after, time.Until(plan.RetainedUntil))
	}
	if user.Spec.Password.RotateEvery != "" && user.Status.Password.RotatedAt != nil {
		if every, err := validate.ParseRotateEvery(user.Spec.Password.RotateEvery, password.DefaultRetention); err == nil && every > 0 {
			after = sooner(after, time.Until(user.Status.Password.RotatedAt.Add(every)))
		}
	}
	if after < 5*time.Second {
		return 5 * time.Second
	}
	return after
}

func sooner(current, wait time.Duration) time.Duration {
	if wait > 0 && wait < current {
		return wait
	}
	return current
}

func (r *MySQLUserReconciler) countRetained(ctx context.Context) {
	var list mysqlv1alpha1.MySQLUserList
	if err := r.List(ctx, &list); err != nil {
		return
	}
	count := 0
	now := time.Now()
	for _, item := range list.Items {
		if item.Status.Password.RetainedUntil != nil && item.Status.Password.RetainedUntil.After(now) {
			count++
		}
	}
	metrics.RetainedPasswords.Set(float64(count))
}

func (r *MySQLUserReconciler) event(obj runtime.Object, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(obj, nil, eventType, reason, reason, "%s", message)
	}
}

func (r *MySQLUserReconciler) access() clusterAccess {
	return clusterAccess{Client: r.Client, APIReader: r.APIReader, Opener: r.Opener, OperatorNamespace: r.OperatorNamespace}
}

// SetupWithManager sets up the controller with the Manager.
func (r *MySQLUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mysqlv1alpha1.MySQLUser{}).
		Watches(&mysqlv1alpha1.MySQLCluster{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			return mapByCluster(ctx, r.Client, r.OperatorNamespace, obj, false)
		})).
		Watches(&mysqlv1alpha1.MySQLTenant{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			return mapByTenant(ctx, r.Client, obj, false)
		})).
		Named("mysqluser").
		Complete(r)
}
