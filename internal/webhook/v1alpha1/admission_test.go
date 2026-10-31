package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/runtimeconfig"
)

var _ = Describe("admission validation", func() {
	It("rejects an explicit cluster that does not mark a master", func() {
		cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{
			Members:           []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
			CredentialsSecret: mysqlv1alpha1.AdminSecretRef{Name: "admin"},
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
				CredentialsSecret: mysqlv1alpha1.AdminSecretRef{Name: "admin"},
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

		allowed := &mysqlv1alpha1.MySQLDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "payments-main", Namespace: "payments"},
			Spec:       mysqlv1alpha1.MySQLDatabaseSpec{Cluster: "db1", Name: "payments_main"},
		}
		warnings, err := (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, allowed)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(ContainElement(ContainSubstring("spec.cluster is deprecated")))

		allowedUser := &mysqlv1alpha1.MySQLUser{
			ObjectMeta: metav1.ObjectMeta{Name: "payments-app", Namespace: "payments"},
			Spec: mysqlv1alpha1.MySQLUserSpec{
				Cluster: "db1", Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
				RequireTLS: true, Secret: mysqlv1alpha1.SecretTemplate{Name: "app"},
				Grants: []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
			},
		}
		warnings, err = (&MySQLUserCustomValidator{Client: k8sClient}).ValidateCreate(ctx, allowedUser)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(ContainElement(ContainSubstring("spec.cluster is deprecated")))
	})

	It("rejects a database or user that sets both cluster fields or neither", func() {
		database := &mysqlv1alpha1.MySQLDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "both", Namespace: "payments"},
			Spec: mysqlv1alpha1.MySQLDatabaseSpec{
				Cluster:    "db1",
				ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1"},
				Name:       "payments_both",
			},
		}
		_, err := (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, database)
		Expect(err).To(HaveOccurred())

		database.Spec.Cluster = ""
		database.Spec.ClusterRef = nil
		_, err = (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, database)
		Expect(err).To(HaveOccurred())

		user := &mysqlv1alpha1.MySQLUser{
			ObjectMeta: metav1.ObjectMeta{Name: "both", Namespace: "payments"},
			Spec: mysqlv1alpha1.MySQLUserSpec{
				Cluster: "db1", ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1"},
				Username: "payments_app", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
				Secret: mysqlv1alpha1.SecretTemplate{Name: "app"},
			},
		}
		_, err = (&MySQLUserCustomValidator{Client: k8sClient}).ValidateCreate(ctx, user)
		Expect(err).To(HaveOccurred())

		user.Spec.Cluster = ""
		user.Spec.ClusterRef = nil
		_, err = (&MySQLUserCustomValidator{Client: k8sClient}).ValidateCreate(ctx, user)
		Expect(err).To(HaveOccurred())
	})

	It("allows SameNamespace without a tenant and does not open the operator cluster", func() {
		runtimeconfig.OperatorNamespace = "default"
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "apps"}})).To(Succeed())
		operatorCluster := &mysqlv1alpha1.MySQLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: "default"},
			Spec: mysqlv1alpha1.MySQLClusterSpec{
				Members:           []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
				CredentialsSecret: mysqlv1alpha1.AdminSecretRef{Name: "admin"},
				Master:            mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
			},
		}
		if err := k8sClient.Create(ctx, operatorCluster); err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(k8sClient.Create(ctx, &mysqlv1alpha1.MySQLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: "apps"},
			Spec: mysqlv1alpha1.MySQLClusterSpec{
				Members:           []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
				CredentialsSecret: mysqlv1alpha1.AdminSecretRef{Name: "admin"},
				Master:            mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
			},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &mysqlv1alpha1.MySQLTenant{
			ObjectMeta: metav1.ObjectMeta{Name: "apps", Namespace: "apps"},
			Spec: mysqlv1alpha1.MySQLTenantSpec{
				Namespaces: []string{"apps"},
				Access: []mysqlv1alpha1.TenantAccess{{
					Cluster:    "db1",
					Databases:  mysqlv1alpha1.PatternRule{Patterns: []string{"*"}},
					Users:      mysqlv1alpha1.PatternRule{Patterns: []string{"*"}},
					Hosts:      []string{"%"},
					Targets:    []mysqlv1alpha1.ApplyTarget{mysqlv1alpha1.ApplyTargetMaster},
					Privileges: []string{"ALL"},
				}},
			},
		})).To(Succeed())

		local := &mysqlv1alpha1.MySQLDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "app-main", Namespace: "apps"},
			Spec: mysqlv1alpha1.MySQLDatabaseSpec{
				Name:       "app_main",
				ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
			},
		}
		warnings, err := (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, local)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeEmpty())

		shared := local.DeepCopy()
		shared.Spec.ClusterRef.NamespaceFrom = mysqlv1alpha1.NamespaceFromOperator
		_, err = (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, shared)
		Expect(err).To(HaveOccurred())

		system := local.DeepCopy()
		system.Spec.Name = "mysql"
		_, err = (&MySQLDatabaseCustomValidator{Client: k8sClient}).ValidateCreate(ctx, system)
		Expect(err).To(HaveOccurred())

		user := &mysqlv1alpha1.MySQLUser{
			ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "apps"},
			Spec: mysqlv1alpha1.MySQLUserSpec{
				Username: "app_user", AuthPlugin: mysqlv1alpha1.AuthPluginCachingSHA2,
				ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
				Grants:     []mysqlv1alpha1.GrantSpec{{Database: "app_main", Privileges: []string{"SUPER"}}},
				Secret:     mysqlv1alpha1.SecretTemplate{Name: "app"},
			},
		}
		_, err = (&MySQLUserCustomValidator{Client: k8sClient}).ValidateCreate(ctx, user)
		Expect(err).To(HaveOccurred())

		user.Spec.Grants[0].Privileges = []string{"SELECT"}
		warnings, err = (&MySQLUserCustomValidator{Client: k8sClient}).ValidateCreate(ctx, user)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeEmpty())
	})
})
