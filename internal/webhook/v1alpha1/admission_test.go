package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/runtimeconfig"
)

var _ = Describe("admission validation", func() {
	It("rejects an explicit cluster that does not mark a master", func() {
		cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{
			Members:           []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
			CredentialsSecret: mysqlv1alpha1.CredentialsSecretRef{Name: "admin"},
			Master:            mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeExplicit},
		}}
		_, err := (&MySQLClusterCustomValidator{}).ValidateCreate(ctx, cluster)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a tenant that allows a forbidden privilege", func() {
		tenant := &mysqlv1alpha1.MySQLTenant{Spec: mysqlv1alpha1.MySQLTenantSpec{
			Namespaces: []string{"payments"},
			Access: []mysqlv1alpha1.TenantAccess{{
				Cluster:    "db1",
				Databases:  mysqlv1alpha1.PatternRule{Patterns: []string{"payments_*"}},
				Users:      mysqlv1alpha1.PatternRule{Patterns: []string{"payments_*"}},
				Privileges: []string{"SUPER"},
			}},
		}}
		_, err := (&MySQLTenantCustomValidator{}).ValidateCreate(ctx, tenant)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a database the tenant policy does not allow", func() {
		runtimeconfig.OperatorNamespace = "default"
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "payments"}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &mysqlv1alpha1.MySQLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: "default"},
			Spec: mysqlv1alpha1.MySQLClusterSpec{
				Members:           []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
				CredentialsSecret: mysqlv1alpha1.CredentialsSecretRef{Name: "admin"},
				Master:            mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
			},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &mysqlv1alpha1.MySQLTenant{
			ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "default"},
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
		})).To(Succeed())

		database := &mysqlv1alpha1.MySQLDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "payments"},
			Spec:       mysqlv1alpha1.MySQLDatabaseSpec{Cluster: "db1", Name: "other_db"},
		}
		_, err := (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, database)
		Expect(err).To(HaveOccurred())

		user := &mysqlv1alpha1.MySQLUser{
			ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "payments"},
			Spec: mysqlv1alpha1.MySQLUserSpec{
				Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
				RequireTLS: false, Secret: mysqlv1alpha1.SecretTemplate{Name: "app"},
			},
		}
		_, err = (&MySQLUserCustomValidator{Client: k8sClient}).ValidateCreate(ctx, user)
		Expect(err).To(HaveOccurred())
	})
})
