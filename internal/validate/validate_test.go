package validate

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

func TestClusterExplicitAndDetected(t *testing.T) {
	explicit := baseCluster()
	explicit.Spec.Master.Mode = mysqlv1alpha1.MasterModeExplicit
	explicit.Spec.Members[0].Master = true
	if err := Cluster(explicit); err != nil {
		t.Fatal(err)
	}
	detected := baseCluster()
	detected.Spec.Members[0].Master = true
	if err := Cluster(detected); err == nil {
		t.Fatal("detected mode accepted an explicit master")
	}
	all := baseCluster()
	all.Spec.DefaultTarget = mysqlv1alpha1.ApplyTargetAll
	if err := Cluster(all); err == nil {
		t.Fatal("replicated cluster accepted target All")
	}
}

func TestTenantRejectsSuper(t *testing.T) {
	tenant := baseTenant()
	tenant.Spec.Access[0].Privileges = []string{"SUPER"}
	if err := Tenant(tenant); err == nil {
		t.Fatal("expected forbidden privilege")
	}
}

func TestDatabaseTenantBoundary(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cluster := baseCluster()
	tenant := baseTenant()
	other := baseTenant()
	other.Name = "other"
	other.Spec.Namespaces = []string{"other"}
	other.Spec.Access[0].Databases.Patterns = []string{"other_*"}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, tenant, other).Build()

	allowed := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "payments-main", Namespace: "payments"}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Cluster: "db1", Name: "payments_main", Target: mysqlv1alpha1.ApplyTargetMaster,
	}}
	if err := Database(context.Background(), cl, "mysql-provisioning-system", allowed, true); err != nil {
		t.Fatal(err)
	}
	denied := allowed.DeepCopy()
	denied.Namespace = "other"
	denied.Spec.Name = "payments_main"
	if err := Database(context.Background(), cl, "mysql-provisioning-system", denied, true); err == nil {
		t.Fatal("other namespace created a payments database")
	}
	system := allowed.DeepCopy()
	system.Spec.Name = "mysql"
	if err := Database(context.Background(), cl, "mysql-provisioning-system", system, true); err == nil {
		t.Fatal("system schema was allowed")
	}
}

func TestUserRequiresTLSAndRetention(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseCluster(), baseTenant()).Build()
	user := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "payments"}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Hosts: []string{"%"}, Target: mysqlv1alpha1.ApplyTargetMaster,
		Password: mysqlv1alpha1.PasswordSpec{Retention: "5m", Generation: 1},
		Grants:   []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
		Secret:   mysqlv1alpha1.SecretTemplate{Name: "payments-app", Data: map[string]string{"USER": "{{ .Username }}"}},
	}}
	if err := User(context.Background(), cl, "mysql-provisioning-system", user, true); err == nil {
		t.Fatal("missing REQUIRE SSL was allowed")
	}
	user.Spec.RequireTLS = true
	if err := User(context.Background(), cl, "mysql-provisioning-system", user, true); err != nil {
		t.Fatal(err)
	}
	user.Spec.Password.Retention = "1m"
	if err := User(context.Background(), cl, "mysql-provisioning-system", user, true); err == nil {
		t.Fatal("retention below 5m was allowed")
	}
	user.Spec.Password.Retention = "5m"
	user.Spec.Password.RotateEvery = "5m"
	if err := User(context.Background(), cl, "mysql-provisioning-system", user, true); err == nil {
		t.Fatal("rotateEvery equal to retention was allowed")
	}
	user.Spec.Grants[0].Privileges = []string{"SUPER"}
	user.Spec.Password.RotateEvery = ""
	if err := User(context.Background(), cl, "mysql-provisioning-system", user, true); err == nil {
		t.Fatal("SUPER grant was allowed")
	}
}

func TestParseRetentionDefaults(t *testing.T) {
	d, err := ParseRetention("")
	if err != nil || d != 24*time.Hour {
		t.Fatalf("default = %s %v", d, err)
	}
	if _, err := ParseRetention("4m"); err == nil {
		t.Fatal("expected minimum")
	}
}

func baseCluster() *mysqlv1alpha1.MySQLCluster {
	return &mysqlv1alpha1.MySQLCluster{ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: "mysql-provisioning-system"}, Spec: mysqlv1alpha1.MySQLClusterSpec{
		Members:              []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
		CredentialsSecret:    mysqlv1alpha1.CredentialsSecretRef{Name: "db1-admin"},
		PrivilegeReplication: mysqlv1alpha1.PrivilegeReplicationReplicated,
		DefaultTarget:        mysqlv1alpha1.ApplyTargetMaster,
		Master:               mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
	}}
}

func baseTenant() *mysqlv1alpha1.MySQLTenant {
	return &mysqlv1alpha1.MySQLTenant{ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "mysql-provisioning-system"}, Spec: mysqlv1alpha1.MySQLTenantSpec{
		Namespaces: []string{"payments"},
		Access: []mysqlv1alpha1.TenantAccess{{
			Cluster:    "db1",
			Databases:  mysqlv1alpha1.PatternRule{Type: mysqlv1alpha1.PatternGlob, Patterns: []string{"payments_*"}},
			Users:      mysqlv1alpha1.PatternRule{Type: mysqlv1alpha1.PatternGlob, Patterns: []string{"payments_*"}},
			Hosts:      []string{"%"},
			Targets:    []mysqlv1alpha1.ApplyTarget{mysqlv1alpha1.ApplyTargetMaster},
			RequireTLS: mysqlv1alpha1.TLSRequired,
			Privileges: []string{"SELECT", "INSERT"},
		}},
	}}
}
