//go:build integration

package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysqltest"
)

func TestOperatorMySQL(t *testing.T) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	k8s := startEnvtest(t)
	server := mysqltest.Start(t, ctx)
	const operatorNS = "mysql-system"
	ensureNamespace(t, ctx, k8s, operatorNS)
	ensureNamespace(t, ctx, k8s, "payments")

	if err := k8s.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db1-admin", Namespace: operatorNS},
		Data:       map[string][]byte{"username": []byte("root"), "password": []byte(server.Admin.Password)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := k8s.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db1-ca", Namespace: operatorNS},
		Data:       map[string][]byte{"ca.crt": server.CA},
	}); err != nil {
		t.Fatal(err)
	}
	if err := k8s.Create(ctx, &mysqlv1alpha1.MySQLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: operatorNS},
		Spec: mysqlv1alpha1.MySQLClusterSpec{
			Members:              []mysqlv1alpha1.MySQLMember{server.Member},
			CredentialsSecret:    mysqlv1alpha1.CredentialsSecretRef{Name: "db1-admin"},
			PrivilegeReplication: mysqlv1alpha1.PrivilegeReplicationReplicated,
			DefaultTarget:        mysqlv1alpha1.ApplyTargetMaster,
			Master:               mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
			TLS: &mysqlv1alpha1.ClusterTLS{
				Mode:       mysqlv1alpha1.TLSModeRequired,
				CASecret:   &mysqlv1alpha1.ObjectKeyRef{Name: "db1-ca"},
				ServerName: "localhost",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := k8s.Create(ctx, &mysqlv1alpha1.MySQLTenant{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: operatorNS},
		Spec: mysqlv1alpha1.MySQLTenantSpec{
			Namespaces: []string{"payments"},
			Access: []mysqlv1alpha1.TenantAccess{{
				Cluster:    "db1",
				Databases:  mysqlv1alpha1.PatternRule{Patterns: []string{"payments_*"}},
				Users:      mysqlv1alpha1.PatternRule{Patterns: []string{"payments_*"}},
				Hosts:      []string{"%"},
				Targets:    []mysqlv1alpha1.ApplyTarget{mysqlv1alpha1.ApplyTargetMaster},
				RequireTLS: mysqlv1alpha1.TLSRequired,
				Privileges: []string{"SELECT"},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	clusterReconciler := &MySQLClusterReconciler{Client: k8s, Scheme: scheme.Scheme, APIReader: k8s, Opener: mysql.SQLOpener{}, OperatorNamespace: operatorNS}
	databaseReconciler := &MySQLDatabaseReconciler{Client: k8s, Scheme: scheme.Scheme, APIReader: k8s, Opener: mysql.SQLOpener{}, OperatorNamespace: operatorNS}
	userReconciler := &MySQLUserReconciler{Client: k8s, Scheme: scheme.Scheme, APIReader: k8s, Opener: mysql.SQLOpener{}, OperatorNamespace: operatorNS}

	clusterKey := types.NamespacedName{Name: "db1", Namespace: operatorNS}
	mustReconcile(t, ctx, clusterReconciler, clusterKey)
	var cluster mysqlv1alpha1.MySQLCluster
	if err := k8s.Get(ctx, clusterKey, &cluster); err != nil {
		t.Fatal(err)
	}
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, mysqlv1alpha1.ConditionReady) || cluster.Status.Master == nil || cluster.Status.Master.Name != server.Member.Name {
		t.Fatalf("cluster status: master=%v conditions=%v", cluster.Status.Master, cluster.Status.Conditions)
	}

	if err := k8s.Create(ctx, &mysqlv1alpha1.MySQLDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-main", Namespace: "payments"},
		Spec: mysqlv1alpha1.MySQLDatabaseSpec{
			Cluster: "db1", Name: "payments_main", Target: mysqlv1alpha1.ApplyTargetMaster,
			CharacterSet: "utf8mb3", Collation: "utf8mb3_general_ci",
		},
	}); err != nil {
		t.Fatal(err)
	}
	databaseKey := types.NamespacedName{Name: "payments-main", Namespace: "payments"}
	mustReconcile(t, ctx, databaseReconciler, databaseKey)
	requireReady(t, ctx, k8s, databaseKey, &mysqlv1alpha1.MySQLDatabase{})
	charset, collation := schemaCharset(t, ctx, server)
	if !schemaCharsetMatches(charset, collation) {
		t.Fatalf("schema charset=%s collation=%s", charset, collation)
	}
	mustReconcile(t, ctx, databaseReconciler, databaseKey)
	charset, collation = schemaCharset(t, ctx, server)
	if !schemaCharsetMatches(charset, collation) {
		t.Fatalf("schema changed after a second reconcile: charset=%s collation=%s", charset, collation)
	}

	if err := k8s.Create(ctx, &mysqlv1alpha1.MySQLUser{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-app", Namespace: "payments"},
		Spec: mysqlv1alpha1.MySQLUserSpec{
			Cluster: "db1", Username: "payments_app", Hosts: []string{"%"},
			AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2, RequireTLS: true,
			Target:   mysqlv1alpha1.ApplyTargetMaster,
			Password: mysqlv1alpha1.PasswordSpec{Retention: "5m", Generation: 1},
			Grants:   []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
			Secret:   mysqlv1alpha1.SecretTemplate{Name: "payments-app"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	userKey := types.NamespacedName{Name: "payments-app", Namespace: "payments"}
	password := ""
	for range 4 {
		mustReconcile(t, ctx, userReconciler, userKey)
		password = secretValue(t, ctx, k8s, "payments-app", mysqlv1alpha1.SecretKeyPassword)
		if password != "" {
			break
		}
	}
	if password == "" {
		t.Fatal("application secret has no password")
	}
	requireReady(t, ctx, k8s, userKey, &mysqlv1alpha1.MySQLUser{})
	assertLogin(t, ctx, server, password, true)
	assertLogin(t, ctx, server, password, false)

	var user mysqlv1alpha1.MySQLUser
	if err := k8s.Get(ctx, userKey, &user); err != nil {
		t.Fatal(err)
	}
	user.Spec.Password.Generation = 2
	if err := k8s.Update(ctx, &user); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, ctx, userReconciler, userKey)
	rotated := secretValue(t, ctx, k8s, "payments-app", mysqlv1alpha1.SecretKeyPassword)
	previous := secretValue(t, ctx, k8s, "payments-app", mysqlv1alpha1.SecretKeyPasswordPrevious)
	if rotated == "" || previous == "" || rotated == previous {
		t.Fatalf("rotation did not publish both passwords")
	}
	if previous != password {
		t.Fatal("previous password is not the original secret value")
	}
	assertLogin(t, ctx, server, previous, true)
	assertLogin(t, ctx, server, rotated, true)

	if err := k8s.Delete(ctx, &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: userKey.Name, Namespace: userKey.Namespace}}); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, ctx, userReconciler, userKey)
	if _, err := server.TryOpen(ctx, accountSession(server, rotated, true)); err == nil {
		t.Fatal("dropped account can still connect")
	}

	var database mysqlv1alpha1.MySQLDatabase
	if err := k8s.Get(ctx, databaseKey, &database); err != nil {
		t.Fatal(err)
	}
	if err := k8s.Delete(ctx, &database); err != nil {
		t.Fatal(err)
	}
	mustReconcile(t, ctx, databaseReconciler, databaseKey)
	charset, _ = schemaCharset(t, ctx, server)
	if charset == "" {
		t.Fatal("deleting MySQLDatabase dropped the schema")
	}
}

func startEnvtest(t *testing.T) client.Client {
	t.Helper()
	if err := mysqlv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})
	k8s, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatal(err)
	}
	return k8s
}

func ensureNamespace(t *testing.T, ctx context.Context, k8s client.Client, name string) {
	t.Helper()
	if err := k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
		t.Fatal(err)
	}
}

type reconciler interface {
	Reconcile(context.Context, reconcile.Request) (reconcile.Result, error)
}

func mustReconcile(t *testing.T, ctx context.Context, r reconciler, key types.NamespacedName) {
	t.Helper()
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
}

func requireReady(t *testing.T, ctx context.Context, k8s client.Client, key types.NamespacedName, obj client.Object) {
	t.Helper()
	if err := k8s.Get(ctx, key, obj); err != nil {
		t.Fatal(err)
	}
	conditions := statusConditions(obj)
	if !meta.IsStatusConditionTrue(conditions, mysqlv1alpha1.ConditionReady) {
		t.Fatalf("%s is not Ready: %v", key, conditions)
	}
}

func statusConditions(obj client.Object) []metav1.Condition {
	switch item := obj.(type) {
	case *mysqlv1alpha1.MySQLDatabase:
		return item.Status.Conditions
	case *mysqlv1alpha1.MySQLUser:
		return item.Status.Conditions
	default:
		return nil
	}
}

func secretValue(t *testing.T, ctx context.Context, k8s client.Client, name, key string) string {
	t.Helper()
	var secret corev1.Secret
	if err := k8s.Get(ctx, types.NamespacedName{Name: name, Namespace: "payments"}, &secret); err != nil {
		return ""
	}
	return string(secret.Data[key])
}

func assertLogin(t *testing.T, ctx context.Context, server *mysqltest.Server, password string, tls bool) {
	t.Helper()
	db, err := server.TryOpen(ctx, accountSession(server, password, tls))
	if tls {
		if err != nil {
			t.Fatalf("login over TLS: %v", err)
		}
		_ = db.Close()
		return
	}
	if err == nil {
		_ = db.Close()
		t.Fatal("REQUIRE SSL account connected without TLS")
	}
}

func accountSession(server *mysqltest.Server, password string, tls bool) mysql.Session {
	session := server.Admin
	session.User = "payments_app"
	session.Password = password
	if !tls {
		session.TLS = nil
		session.CA = nil
	}
	return session
}

func schemaCharsetMatches(charset, collation string) bool {
	return (charset == "utf8mb3" || charset == "utf8") && collation == "utf8mb3_general_ci"
}

func schemaCharset(t *testing.T, ctx context.Context, server *mysqltest.Server) (string, string) {
	t.Helper()
	db := server.Open(t, ctx, server.Admin)
	defer func() { _ = db.Close() }()
	rows, err := db.Query(ctx, "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = "+mysql.QuoteLiteral("payments_main"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", ""
	}
	var charset, collation string
	if err := rows.Scan(&charset, &collation); err != nil {
		t.Fatal(err)
	}
	return charset, collation
}
