package validate

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

const (
	testOperatorNamespace = "mysql-provisioning-system"
	testPaymentsNamespace = "payments"
	mysqlSchema           = "mysql"
)

func TestClusterCredentialKeys(t *testing.T) {
	ok := baseCluster()
	ok.Spec.CredentialsSecret.UsernameKey = "MYSQL_USER"
	ok.Spec.CredentialsSecret.PasswordKey = "MYSQL_PASSWORD"
	if err := Cluster(ok); err != nil {
		t.Fatal(err)
	}
	same := baseCluster()
	same.Spec.CredentialsSecret.UsernameKey = "password"
	if err := Cluster(same); err == nil {
		t.Fatal("usernameKey password was accepted with the default password key")
	}
	bad := baseCluster()
	bad.Spec.CredentialsSecret.UsernameKey = "user name"
	if err := Cluster(bad); err == nil {
		t.Fatal("usernameKey with a space was accepted")
	}
}

func TestClusterAttributes(t *testing.T) {
	ok := baseCluster()
	ok.Spec.Members[0].Attributes = map[string]string{"publicHost": "db.example.com"}
	if err := Cluster(ok); err != nil {
		t.Fatal(err)
	}
	badKey := baseCluster()
	badKey.Spec.Members[0].Attributes = map[string]string{"public-host": "db.example.com"}
	if err := Cluster(badKey); err == nil {
		t.Fatal("attribute key with a hyphen was accepted")
	}
	tooLong := baseCluster()
	tooLong.Spec.Members[0].Attributes = map[string]string{"publicHost": strings.Repeat("a", 254)}
	if err := Cluster(tooLong); err == nil {
		t.Fatal("attribute value longer than 253 characters was accepted")
	}
}

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

func TestClusterTLS(t *testing.T) {
	disabled := baseCluster()
	disabled.Spec.TLS = &mysqlv1alpha1.ClusterTLS{CASecret: &mysqlv1alpha1.ObjectKeyRef{Name: "ca"}}
	if err := Cluster(disabled); err == nil {
		t.Fatal("plaintext cluster accepted a CA secret")
	}
	skip := baseCluster()
	skip.Spec.TLS = &mysqlv1alpha1.ClusterTLS{
		Mode:     mysqlv1alpha1.TLSModeSkipVerify,
		CASecret: &mysqlv1alpha1.ObjectKeyRef{Name: "ca"},
	}
	if err := Cluster(skip); err == nil {
		t.Fatal("skip-verify accepted a CA secret")
	}
	required := baseCluster()
	required.Spec.TLS = &mysqlv1alpha1.ClusterTLS{
		Mode:       mysqlv1alpha1.TLSModeRequired,
		CASecret:   &mysqlv1alpha1.ObjectKeyRef{Name: "db1-ca", Key: "root.pem"},
		ServerName: "db.internal",
	}
	if err := Cluster(required); err != nil {
		t.Fatal(err)
	}
	both := baseCluster()
	both.Spec.TLS = &mysqlv1alpha1.ClusterTLS{
		Mode:        mysqlv1alpha1.TLSModeRequired,
		CASecret:    &mysqlv1alpha1.ObjectKeyRef{Name: "db1-ca"},
		CAConfigMap: &mysqlv1alpha1.ObjectKeyRef{Name: "db1-ca", Key: "ca.pem"},
	}
	if err := Cluster(both); err == nil {
		t.Fatal("both CA sources were accepted")
	}
	fromConfigMap := baseCluster()
	fromConfigMap.Spec.TLS = &mysqlv1alpha1.ClusterTLS{
		Mode:        mysqlv1alpha1.TLSModeRequired,
		CAConfigMap: &mysqlv1alpha1.ObjectKeyRef{Name: "db1-ca", Key: "ca.pem"},
	}
	if err := Cluster(fromConfigMap); err != nil {
		t.Fatal(err)
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

	allowed := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "payments-main", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Cluster: "db1", Name: "payments_main", Target: mysqlv1alpha1.ApplyTargetMaster,
	}}
	if err := Database(context.Background(), cl, testOperatorNamespace, allowed, true); err != nil {
		t.Fatal(err)
	}
	denied := allowed.DeepCopy()
	denied.Namespace = "other"
	denied.Spec.Name = "payments_main"
	if err := Database(context.Background(), cl, testOperatorNamespace, denied, true); err == nil {
		t.Fatal("other namespace created a payments database")
	}
	system := allowed.DeepCopy()
	system.Spec.Name = mysqlSchema
	if err := Database(context.Background(), cl, testOperatorNamespace, system, true); err == nil {
		t.Fatal("system schema was allowed")
	}
}

func TestUserRequiresTLSAndRetention(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseCluster(), baseTenant()).Build()
	user := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Hosts: []string{"%"}, Target: mysqlv1alpha1.ApplyTargetMaster,
		Password: mysqlv1alpha1.PasswordSpec{Retention: "5m", Generation: 1},
		Grants:   []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
		Secret:   mysqlv1alpha1.SecretTemplate{Name: "payments-app", Data: map[string]string{"USER": "{{ .Username }}"}},
	}}
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err == nil {
		t.Fatal("missing REQUIRE SSL was allowed")
	}
	user.Spec.RequireTLS = true
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err != nil {
		t.Fatal(err)
	}
	user.Spec.Password.Retention = "1m"
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err == nil {
		t.Fatal("retention below 5m was allowed")
	}
	user.Spec.Password.Retention = "5m"
	user.Spec.Password.RotateEvery = "5m"
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err == nil {
		t.Fatal("rotateEvery equal to retention was allowed")
	}
	user.Spec.Grants[0].Privileges = []string{"SUPER"}
	user.Spec.Password.RotateEvery = ""
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err == nil {
		t.Fatal("SUPER grant was allowed")
	}
}

func TestAllPrivilegeOnTenantDatabase(t *testing.T) {
	tenant := baseTenant()
	tenant.Spec.Access[0].Privileges = []string{"ALL"}
	if err := Tenant(tenant); err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseCluster(), tenant).Build()
	user := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Hosts: []string{"%"}, RequireTLS: true, Target: mysqlv1alpha1.ApplyTargetMaster,
		Password: mysqlv1alpha1.PasswordSpec{Retention: "24h", Generation: 1},
		Grants:   []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"ALL"}}},
		Secret:   mysqlv1alpha1.SecretTemplate{Name: "payments-app"},
	}}
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err != nil {
		t.Fatal(err)
	}
	user.Spec.Grants[0].Database = mysqlSchema
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err == nil {
		t.Fatal("ALL on the mysql schema was allowed")
	}
	narrow := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseCluster(), baseTenant()).Build()
	user.Spec.Grants[0].Database = "payments_main"
	if err := User(context.Background(), narrow, testOperatorNamespace, user, true); err == nil {
		t.Fatal("ALL was allowed without a tenant privilege")
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
	return &mysqlv1alpha1.MySQLCluster{ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: testOperatorNamespace}, Spec: mysqlv1alpha1.MySQLClusterSpec{
		Members:              []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
		CredentialsSecret:    mysqlv1alpha1.AdminSecretRef{Name: "db1-admin"},
		PrivilegeReplication: mysqlv1alpha1.PrivilegeReplicationReplicated,
		DefaultTarget:        mysqlv1alpha1.ApplyTargetMaster,
		Master:               mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
	}}
}

func TestUsernameSuffixLength(t *testing.T) {
	n, err := UsernameSuffixLength(nil)
	if err != nil || n != 0 {
		t.Fatalf("missing suffix length %d: %v", n, err)
	}
	n, err = UsernameSuffixLength(&mysqlv1alpha1.UsernameSuffix{})
	if err != nil || n != DefaultUsernameSuffixLength {
		t.Fatalf("default suffix length %d: %v", n, err)
	}
	n, err = UsernameSuffixLength(&mysqlv1alpha1.UsernameSuffix{Length: 4})
	if err != nil || n != 4 {
		t.Fatalf("explicit suffix length %d: %v", n, err)
	}
	if _, err := UsernameSuffixLength(&mysqlv1alpha1.UsernameSuffix{Length: 32}); err == nil {
		t.Fatal("suffix length 32 was allowed")
	}

	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseCluster(), baseTenant()).Build()
	user := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Hosts: []string{"%"}, RequireTLS: true, Target: mysqlv1alpha1.ApplyTargetMaster,
		Password:       mysqlv1alpha1.PasswordSpec{Retention: "24h", Generation: 1},
		Grants:         []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
		Secret:         mysqlv1alpha1.SecretTemplate{Name: "payments-app"},
		UsernameSuffix: &mysqlv1alpha1.UsernameSuffix{Length: 8},
	}}
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err != nil {
		t.Fatal(err)
	}
	user.Spec.Username = "payments_app_payments_app"
	if err := User(context.Background(), cl, testOperatorNamespace, user, true); err == nil {
		t.Fatal("username plus suffix longer than 32 characters was allowed")
	}
}

func TestResolveCluster(t *testing.T) {
	op := testOperatorNamespace
	binding, err := ResolveCluster(testPaymentsNamespace, op, "db1", nil)
	if err != nil || binding.Name != "db1" || binding.Namespace != op || binding.SameNamespace {
		t.Fatalf("spec.cluster binding %#v err %v", binding, err)
	}
	binding, err = ResolveCluster(testPaymentsNamespace, op, "", &mysqlv1alpha1.ClusterRef{Name: "db1"})
	if err != nil || binding.Namespace != op || binding.SameNamespace {
		t.Fatalf("default namespaceFrom %#v err %v", binding, err)
	}
	binding, err = ResolveCluster(testPaymentsNamespace, op, "", &mysqlv1alpha1.ClusterRef{Name: "app", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame})
	if err != nil || binding.Namespace != testPaymentsNamespace || !binding.SameNamespace || binding.Name != "app" {
		t.Fatalf("SameNamespace %#v err %v", binding, err)
	}
	if _, err := ResolveCluster(testPaymentsNamespace, op, "db1", &mysqlv1alpha1.ClusterRef{Name: "db1"}); err == nil {
		t.Fatal("both fields were accepted")
	}
	if _, err := ResolveCluster(testPaymentsNamespace, op, "", nil); err == nil {
		t.Fatal("neither field was accepted")
	}
	if warnings := ClusterWarnings("db1", nil); len(warnings) != 1 || warnings[0] != DeprecatedClusterWarning {
		t.Fatalf("warnings %#v", warnings)
	}
	if warnings := ClusterWarnings("", &mysqlv1alpha1.ClusterRef{Name: "db1"}); warnings != nil {
		t.Fatalf("clusterRef warning %#v", warnings)
	}
}

func TestClusterRefTenantBoundary(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	op := testOperatorNamespace
	operatorCluster := baseCluster()
	localCluster := baseCluster()
	localCluster.Namespace = testPaymentsNamespace
	userTenant := baseTenant()
	userTenant.Namespace = testPaymentsNamespace
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorCluster, localCluster, userTenant).Build()

	shared := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "payments-main", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Cluster: "db1", Name: "payments_main", Target: mysqlv1alpha1.ApplyTargetMaster,
	}}
	if err := Database(context.Background(), cl, op, shared, true); err == nil {
		t.Fatal("OperatorNamespace was allowed without a tenant in the operator namespace")
	}
	byRef := shared.DeepCopy()
	byRef.Spec.Cluster = ""
	byRef.Spec.ClusterRef = &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromOperator}
	if err := Database(context.Background(), cl, op, byRef, true); err == nil {
		t.Fatal("clusterRef OperatorNamespace was allowed without a tenant in the operator namespace")
	}

	local := shared.DeepCopy()
	local.Spec.Cluster = ""
	local.Spec.ClusterRef = &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame}
	if err := Database(context.Background(), cl, op, local, true); err != nil {
		t.Fatal(err)
	}
	missingLocal := local.DeepCopy()
	missingLocal.Namespace = "shop"
	if err := Database(context.Background(), cl, op, missingLocal, true); err == nil || !strings.Contains(err.Error(), "shop/db1") {
		t.Fatalf("SameNamespace opened another namespace: %v", err)
	}
	system := local.DeepCopy()
	system.Spec.Name = mysqlSchema
	if err := Database(context.Background(), cl, op, system, true); err == nil {
		t.Fatal("SameNamespace allowed the mysql schema")
	}

	localUser := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Hosts: []string{"%"}, Target: mysqlv1alpha1.ApplyTargetMaster,
		ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
		Grants:     []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SUPER"}}},
		Secret:     mysqlv1alpha1.SecretTemplate{Name: "payments-app"},
	}}
	if err := User(context.Background(), cl, op, localUser, true); err == nil {
		t.Fatal("SameNamespace allowed SUPER")
	}
	localUser.Spec.Grants[0].Privileges = []string{"SELECT"}
	if err := User(context.Background(), cl, op, localUser, true); err != nil {
		t.Fatal(err)
	}

	operatorTenant := baseTenant()
	withTenant := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorCluster, localCluster, userTenant, operatorTenant).Build()
	operatorUser := localUser.DeepCopy()
	operatorUser.Spec.ClusterRef = &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromOperator}
	operatorUser.Spec.RequireTLS = true
	if err := User(context.Background(), withTenant, op, operatorUser, true); err != nil {
		t.Fatal(err)
	}
	legacy := operatorUser.DeepCopy()
	legacy.Spec.ClusterRef = nil
	legacy.Spec.Cluster = "db1"
	if err := User(context.Background(), withTenant, op, legacy, true); err != nil {
		t.Fatal(err)
	}
}

func TestSameNamespaceSkipsTenantQuota(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	op := testOperatorNamespace
	tenant := baseTenant()
	limit := int32(1)
	tenant.Spec.Quotas = &mysqlv1alpha1.TenantQuotas{MaxDatabases: &limit, MaxUsers: &limit}
	operatorCluster := baseCluster()
	localCluster := baseCluster()
	localCluster.Namespace = testPaymentsNamespace
	localDB := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "local-db", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Name: "payments_local", ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
	}}
	localUser := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "local-user", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Username: "payments_local", ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
	}}
	onlyLocal := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorCluster, localCluster, tenant, localDB, localUser).Build()
	shared := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Cluster: "db1", Name: "payments_shared", Target: mysqlv1alpha1.ApplyTargetMaster,
	}}
	if err := Database(context.Background(), onlyLocal, op, shared, true); err != nil {
		t.Fatal(err)
	}
	sharedUser := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "shared-user", Namespace: testPaymentsNamespace}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Hosts: []string{"%"}, RequireTLS: true, Target: mysqlv1alpha1.ApplyTargetMaster,
		Grants: []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
		Secret: mysqlv1alpha1.SecretTemplate{Name: "payments-app"},
	}}
	if err := User(context.Background(), onlyLocal, op, sharedUser, true); err != nil {
		t.Fatal(err)
	}

	existingDB := shared.DeepCopy()
	existingDB.Name = "existing-db"
	existingUser := sharedUser.DeepCopy()
	existingUser.Name = "existing-user"
	atQuota := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorCluster, localCluster, tenant, existingDB, existingUser, localDB).Build()
	second := shared.DeepCopy()
	second.Name = "second"
	second.Spec.Name = "payments_second"
	if err := Database(context.Background(), atQuota, op, second, true); err == nil {
		t.Fatal("OperatorNamespace database was not counted toward the quota")
	}
	anotherLocal := localDB.DeepCopy()
	anotherLocal.Name = "another-local"
	anotherLocal.Spec.Name = "payments_another"
	if err := Database(context.Background(), atQuota, op, anotherLocal, true); err != nil {
		t.Fatal(err)
	}
	secondUser := sharedUser.DeepCopy()
	secondUser.Name = "second-user"
	secondUser.Spec.Username = "payments_next"
	if err := User(context.Background(), atQuota, op, secondUser, true); err == nil {
		t.Fatal("OperatorNamespace user was not counted toward the quota")
	}
}

func baseTenant() *mysqlv1alpha1.MySQLTenant {
	return &mysqlv1alpha1.MySQLTenant{ObjectMeta: metav1.ObjectMeta{Name: testPaymentsNamespace, Namespace: testOperatorNamespace}, Spec: mysqlv1alpha1.MySQLTenantSpec{
		Namespaces: []string{testPaymentsNamespace},
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
