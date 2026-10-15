package controller

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysqltest"
)

var _ = Describe("MySQL controllers", func() {
	It("ignores missing objects", func() {
		missing := types.NamespacedName{Name: "missing", Namespace: "default"}
		_, err := (&MySQLClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), OperatorNamespace: "default"}).Reconcile(ctx, reconcile.Request{NamespacedName: missing})
		Expect(err).NotTo(HaveOccurred())
		_, err = (&MySQLTenantReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), OperatorNamespace: "default"}).Reconcile(ctx, reconcile.Request{NamespacedName: missing})
		Expect(err).NotTo(HaveOccurred())
		_, err = (&MySQLDatabaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), OperatorNamespace: "default"}).Reconcile(ctx, reconcile.Request{NamespacedName: missing})
		Expect(err).NotTo(HaveOccurred())
		_, err = (&MySQLUserReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), OperatorNamespace: "default"}).Reconcile(ctx, reconcile.Request{NamespacedName: missing})
		Expect(err).NotTo(HaveOccurred())
	})

	It("creates a database and user and never drops the database", func() {
		created := false
		script := &mysqltest.Script{
			ExecFunc: func(query string) error {
				if strings.Contains(query, "CREATE DATABASE") {
					created = true
				}
				return nil
			},
			QueryFunc: func(query string) ([][]any, error) {
				switch {
				case strings.Contains(query, "@@global.read_only"):
					return [][]any{{int64(0), int64(0), "11"}}, nil
				case strings.Contains(query, "replication_connection_configuration"):
					return [][]any{{int64(0)}}, nil
				case strings.Contains(query, "VERSION()"):
					return [][]any{{"8.0.36"}}, nil
				case strings.Contains(query, "mysql.user"):
					return nil, nil
				case strings.Contains(query, "SHOW GRANTS"):
					return [][]any{{"GRANT USAGE ON *.* TO `payments_app`@`%`"}}, nil
				case strings.Contains(query, "SCHEMATA"):
					if created {
						return [][]any{{"utf8mb3", "utf8mb3_general_ci"}}, nil
					}
					return nil, nil
				default:
					return nil, nil
				}
			},
		}
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db1-admin", Namespace: "default"},
			Data:       map[string][]byte{"username": []byte("root"), "password": []byte("secret")},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &mysqlv1alpha1.MySQLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: "default"},
			Spec: mysqlv1alpha1.MySQLClusterSpec{
				Members:              []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
				CredentialsSecret:    mysqlv1alpha1.CredentialsSecretRef{Name: "db1-admin"},
				PrivilegeReplication: mysqlv1alpha1.PrivilegeReplicationReplicated,
				DefaultTarget:        mysqlv1alpha1.ApplyTargetMaster,
				Master:               mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
			},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &mysqlv1alpha1.MySQLTenant{
			ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "default"},
			Spec: mysqlv1alpha1.MySQLTenantSpec{
				Namespaces: []string{"default"},
				Access: []mysqlv1alpha1.TenantAccess{{
					Cluster:    "db1",
					Databases:  mysqlv1alpha1.PatternRule{Patterns: []string{"payments_*"}},
					Users:      mysqlv1alpha1.PatternRule{Patterns: []string{"payments_*"}},
					Hosts:      []string{"%"},
					Targets:    []mysqlv1alpha1.ApplyTarget{mysqlv1alpha1.ApplyTargetMaster},
					RequireTLS: mysqlv1alpha1.TLSOptional,
					Privileges: []string{"SELECT"},
				}},
			},
		})).To(Succeed())
		database := &mysqlv1alpha1.MySQLDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "payments-main", Namespace: "default"},
			Spec:       mysqlv1alpha1.MySQLDatabaseSpec{Cluster: "db1", Name: "payments_main", Target: mysqlv1alpha1.ApplyTargetMaster},
		}
		Expect(k8sClient.Create(ctx, database)).To(Succeed())
		dbReconciler := &MySQLDatabaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient, Opener: script, OperatorNamespace: "default"}
		_, err := dbReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: database.Name, Namespace: database.Namespace}})
		Expect(err).NotTo(HaveOccurred())
		Expect(script.Contains("CREATE DATABASE")).To(BeTrue())
		Expect(script.Contains("DROP DATABASE")).To(BeFalse())

		user := &mysqlv1alpha1.MySQLUser{
			ObjectMeta: metav1.ObjectMeta{Name: "payments-app", Namespace: "default"},
			Spec: mysqlv1alpha1.MySQLUserSpec{
				Cluster: "db1", Username: "payments_app", Hosts: []string{"%"},
				AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2, RequireTLS: true,
				Target: mysqlv1alpha1.ApplyTargetMaster,
				Grants: []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
				Secret: mysqlv1alpha1.SecretTemplate{Name: "payments-app", Data: map[string]string{"USER": "{{ .Username }}"}},
			},
		}
		Expect(k8sClient.Create(ctx, user)).To(Succeed())
		userReconciler := &MySQLUserReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient, Opener: script, OperatorNamespace: "default"}
		_, err = userReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: user.Name, Namespace: user.Namespace}})
		Expect(err).NotTo(HaveOccurred())
		_, err = userReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: user.Name, Namespace: user.Namespace}})
		Expect(err).NotTo(HaveOccurred())
		Expect(script.Contains("CREATE USER")).To(BeTrue())
		Expect(script.Contains("REQUIRE SSL")).To(BeTrue())
		Expect(script.Contains("DROP DATABASE")).To(BeFalse())

		var secret corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "payments-app", Namespace: "default"}, &secret)).To(Succeed())
		Expect(secret.Data[mysqlv1alpha1.SecretKeyPassword]).NotTo(BeEmpty())

		Expect(k8sClient.Delete(ctx, user)).To(Succeed())
		_, err = userReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: user.Name, Namespace: user.Namespace}})
		Expect(err).NotTo(HaveOccurred())
		Expect(script.Contains("DROP USER")).To(BeTrue())
		Expect(script.Contains("DROP DATABASE")).To(BeFalse())
	})
})
