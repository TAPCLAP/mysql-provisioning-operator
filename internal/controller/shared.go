package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

const (
	requeueCluster = 30 * time.Second
	requeueObject  = time.Minute
)

type clusterAccess struct {
	Client            client.Client
	APIReader         client.Reader
	Opener            mysql.Opener
	OperatorNamespace string
}

func (c clusterAccess) reader() client.Reader {
	if c.APIReader != nil {
		return c.APIReader
	}
	return c.Client
}

func (c clusterAccess) opener() mysql.Opener {
	if c.Opener != nil {
		return c.Opener
	}
	return mysql.SQLOpener{}
}

type resolved struct {
	Cluster   *mysqlv1alpha1.MySQLCluster
	Selection mysql.Selection
	Members   []mysqlv1alpha1.MySQLMember
	LogBin    bool
	Auth      mysql.Session
}

func (c clusterAccess) resolve(ctx context.Context, clusterName string, target mysqlv1alpha1.ApplyTarget) (resolved, error) {
	var out resolved
	cluster, err := c.loadCluster(ctx, clusterName)
	if err != nil {
		return out, err
	}
	out.Cluster = cluster
	auth, err := readSession(ctx, c.reader(), cluster)
	if err != nil {
		return out, err
	}
	out.Auth = auth
	out.Selection = mysql.SelectMaster(cluster, mysql.ProbeAll(ctx, c.opener(), cluster, auth))
	effective := validate.EffectiveTarget(target, cluster)
	if err := replicationAllowed(cluster, effective); err != nil {
		return out, err
	}
	switch effective {
	case mysqlv1alpha1.ApplyTargetAll:
		out.Members = append([]mysqlv1alpha1.MySQLMember(nil), cluster.Spec.Members...)
		out.LogBin = false
	default:
		if out.Selection.Master == nil {
			return out, masterError(out.Selection)
		}
		out.Members = []mysqlv1alpha1.MySQLMember{*out.Selection.Master}
		out.LogBin = true
	}
	return out, nil
}

func (c clusterAccess) loadCluster(ctx context.Context, name string) (*mysqlv1alpha1.MySQLCluster, error) {
	var cluster mysqlv1alpha1.MySQLCluster
	key := types.NamespacedName{Namespace: c.OperatorNamespace, Name: name}
	if err := c.Client.Get(ctx, key, &cluster); err != nil {
		return nil, fmt.Errorf("get cluster %s: %w", name, err)
	}
	return &cluster, nil
}

func readSession(ctx context.Context, reader client.Reader, cluster *mysqlv1alpha1.MySQLCluster) (mysql.Session, error) {
	var session mysql.Session
	user, password, err := readAdminSecret(ctx, reader, cluster.Namespace, cluster.Spec.CredentialsSecret.Name)
	if err != nil {
		return session, err
	}
	session.User = user
	session.Password = password
	session.TLS = cluster.Spec.TLS
	if session.TLS == nil || !session.TLS.Enabled() {
		return session, nil
	}
	ca, err := readCA(ctx, reader, cluster.Namespace, session.TLS)
	if err != nil {
		return session, err
	}
	session.CA = ca
	if session.TLS.ClientSecret != nil {
		cert, err := readSecretKey(ctx, reader, cluster.Namespace, session.TLS.ClientSecret.Name, "tls.crt")
		if err != nil {
			return session, err
		}
		key, err := readSecretKey(ctx, reader, cluster.Namespace, session.TLS.ClientSecret.Name, "tls.key")
		if err != nil {
			return session, err
		}
		session.ClientCert = cert
		session.ClientKey = key
	}
	return session, nil
}

func readCA(ctx context.Context, reader client.Reader, namespace string, tls *mysqlv1alpha1.ClusterTLS) ([]byte, error) {
	switch {
	case tls.CASecret != nil && tls.CAConfigMap != nil:
		return nil, fmt.Errorf("tls.caSecret and tls.caConfigMap are mutually exclusive")
	case tls.CASecret != nil:
		return readSecretKey(ctx, reader, namespace, tls.CASecret.Name, tls.CASecret.KeyOrDefault("ca.crt"))
	case tls.CAConfigMap != nil:
		return readConfigMapKey(ctx, reader, namespace, tls.CAConfigMap.Name, tls.CAConfigMap.KeyOrDefault("ca.crt"))
	default:
		return nil, nil
	}
}

func readConfigMapKey(ctx context.Context, reader client.Reader, namespace, name, key string) ([]byte, error) {
	var configMap corev1.ConfigMap
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &configMap); err != nil {
		return nil, fmt.Errorf("get configmap %s/%s: %w", namespace, name, err)
	}
	if value := configMap.Data[key]; value != "" {
		return []byte(value), nil
	}
	if value := configMap.BinaryData[key]; len(value) > 0 {
		return value, nil
	}
	return nil, fmt.Errorf("configmap %s/%s must contain %s", namespace, name, key)
}

func readSecretKey(ctx context.Context, reader client.Reader, namespace, name, key string) ([]byte, error) {
	var secret corev1.Secret
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	value := secret.Data[key]
	if len(value) == 0 {
		return nil, fmt.Errorf("secret %s/%s must contain %s", namespace, name, key)
	}
	return value, nil
}

func readAdminSecret(ctx context.Context, reader client.Reader, namespace, name string) (string, string, error) {
	var secret corev1.Secret
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return "", "", fmt.Errorf("get admin secret %s/%s: %w", namespace, name, err)
	}
	user := string(secret.Data["username"])
	password := string(secret.Data["password"])
	if user == "" || password == "" {
		return "", "", fmt.Errorf("admin secret %s/%s must contain username and password", namespace, name)
	}
	return user, password, nil
}

func replicationAllowed(cluster *mysqlv1alpha1.MySQLCluster, target mysqlv1alpha1.ApplyTarget) error {
	replication := cluster.Spec.PrivilegeReplication
	if replication == "" {
		replication = mysqlv1alpha1.PrivilegeReplicationReplicated
	}
	if replication == mysqlv1alpha1.PrivilegeReplicationReplicated && target == mysqlv1alpha1.ApplyTargetAll {
		return fmt.Errorf("target All is incompatible with privilegeReplication Replicated")
	}
	return nil
}

func masterError(sel mysql.Selection) error {
	switch {
	case sel.InvalidCriteria:
		return fmt.Errorf("master criteria are invalid")
	case sel.SplitBrain:
		return fmt.Errorf("more than one master matched")
	case sel.MasterUnavailable:
		return fmt.Errorf("explicit master is unavailable")
	default:
		return fmt.Errorf("no master matched")
	}
}

func masterCondition(sel mysql.Selection) (string, string) {
	switch {
	case sel.InvalidCriteria:
		return mysqlv1alpha1.ConditionNoMaster, "InvalidCriteria"
	case sel.SplitBrain:
		return mysqlv1alpha1.ConditionSplitBrain, "SplitBrain"
	case sel.MasterUnavailable:
		return mysqlv1alpha1.ConditionMasterUnavailable, "MasterUnavailable"
	default:
		return mysqlv1alpha1.ConditionNoMaster, "NoMaster"
	}
}

func setCondition(conditions *[]metav1.Condition, generation int64, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func recordPlacement(target resolved, names []string) ([]mysqlv1alpha1.EndpointStatus, *mysqlv1alpha1.EndpointStatus) {
	return appliedEndpoints(target.Cluster, names), selectedMaster(target)
}

func appliedEndpoints(cluster *mysqlv1alpha1.MySQLCluster, names []string) []mysqlv1alpha1.EndpointStatus {
	if cluster == nil || len(names) == 0 {
		return nil
	}
	byName := make(map[string]mysqlv1alpha1.MySQLMember, len(cluster.Spec.Members))
	for _, member := range cluster.Spec.Members {
		byName[member.Name] = member
	}
	out := make([]mysqlv1alpha1.EndpointStatus, 0, len(names))
	for _, name := range names {
		member, ok := byName[name]
		if !ok {
			out = append(out, mysqlv1alpha1.EndpointStatus{Name: name})
			continue
		}
		out = append(out, endpointStatus(member))
	}
	return out
}

func selectedMaster(target resolved) *mysqlv1alpha1.EndpointStatus {
	if target.Selection.Master == nil {
		return nil
	}
	status := endpointStatus(*target.Selection.Master)
	return &status
}

func endpointStatus(member mysqlv1alpha1.MySQLMember) mysqlv1alpha1.EndpointStatus {
	return mysqlv1alpha1.EndpointStatus{
		Name: member.Name,
		Host: member.Host,
		Port: member.AddressPort(),
	}
}

func applyMembers(ctx context.Context, opener mysql.Opener, target resolved, fn func(context.Context, mysqlv1alpha1.MySQLMember, mysql.DB) error) ([]string, error) {
	var applied []string
	var errs []error
	for _, member := range target.Members {
		db, err := opener.Open(ctx, member, target.Auth)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", member.Name, err))
			continue
		}
		err = mysql.PrepareSession(ctx, db, target.LogBin)
		if err == nil {
			err = fn(ctx, member, db)
		}
		_ = db.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", member.Name, err))
			continue
		}
		applied = append(applied, member.Name)
	}
	return applied, joinErrors(errs)
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", errorText(errs))
}

func errorText(errs []error) string {
	var msg strings.Builder
	for i, err := range errs {
		if i > 0 {
			msg.WriteString("; ")
		}
		msg.WriteString(err.Error())
	}
	return msg.String()
}

func mapByCluster(ctx context.Context, c client.Client, operatorNamespace string, obj client.Object, database bool) []reconcile.Request {
	cluster, ok := obj.(*mysqlv1alpha1.MySQLCluster)
	if !ok || cluster.Namespace != operatorNamespace {
		return nil
	}
	return requestsForCluster(ctx, c, cluster.Name, database)
}

func mapByTenant(ctx context.Context, c client.Client, obj client.Object, database bool) []reconcile.Request {
	tenant, ok := obj.(*mysqlv1alpha1.MySQLTenant)
	if !ok {
		return nil
	}
	var requests []reconcile.Request
	for _, namespace := range tenant.Spec.Namespaces {
		requests = append(requests, requestsInNamespace(ctx, c, namespace, database)...)
	}
	return requests
}

func requestsForCluster(ctx context.Context, c client.Client, clusterName string, database bool) []reconcile.Request {
	if database {
		var list mysqlv1alpha1.MySQLDatabaseList
		if err := c.List(ctx, &list); err != nil {
			return nil
		}
		var requests []reconcile.Request
		for _, item := range list.Items {
			if item.Spec.Cluster == clusterName {
				requests = append(requests, requestFor(&item))
			}
		}
		return requests
	}
	var list mysqlv1alpha1.MySQLUserList
	if err := c.List(ctx, &list); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for _, item := range list.Items {
		if item.Spec.Cluster == clusterName {
			requests = append(requests, requestFor(&item))
		}
	}
	return requests
}

func requestsInNamespace(ctx context.Context, c client.Client, namespace string, database bool) []reconcile.Request {
	if database {
		var list mysqlv1alpha1.MySQLDatabaseList
		if err := c.List(ctx, &list, client.InNamespace(namespace)); err != nil {
			return nil
		}
		requests := make([]reconcile.Request, 0, len(list.Items))
		for i := range list.Items {
			requests = append(requests, requestFor(&list.Items[i]))
		}
		return requests
	}
	var list mysqlv1alpha1.MySQLUserList
	if err := c.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, requestFor(&list.Items[i]))
	}
	return requests
}

func requestFor(obj client.Object) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}}
}

func updateStatus(ctx context.Context, c client.Client, obj client.Object) error {
	return c.Status().Update(ctx, obj)
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
