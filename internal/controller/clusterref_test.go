package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

func TestMapByClusterKeepsNamespacesApart(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mysqlv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	op := "mysql-provisioning-system"
	localCluster := &mysqlv1alpha1.MySQLCluster{ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: "payments"}}
	sharedCluster := &mysqlv1alpha1.MySQLCluster{ObjectMeta: metav1.ObjectMeta{Name: "db1", Namespace: op}}
	same := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "payments"}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Name:       "payments_local",
		ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
	}}
	shared := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "payments"}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Cluster: "db1", Name: "payments_shared",
	}}
	otherNamespace := &mysqlv1alpha1.MySQLDatabase{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "shop"}, Spec: mysqlv1alpha1.MySQLDatabaseSpec{
		Name:       "shop_main",
		ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
	}}
	defaultRef := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "default-ref", Namespace: "payments"}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Username:   "payments_app",
		ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1"},
	}}
	localUser := &mysqlv1alpha1.MySQLUser{ObjectMeta: metav1.ObjectMeta{Name: "local-user", Namespace: "payments"}, Spec: mysqlv1alpha1.MySQLUserSpec{
		Username:   "payments_local",
		ClusterRef: &mysqlv1alpha1.ClusterRef{Name: "db1", NamespaceFrom: mysqlv1alpha1.NamespaceFromSame},
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(same, shared, otherNamespace, defaultRef, localUser).Build()

	if got := requestNames(mapByCluster(context.Background(), cl, op, localCluster, true)); !sameNames(got, []string{"payments/local"}) {
		t.Fatalf("local cluster databases %#v", got)
	}
	if got := requestNames(mapByCluster(context.Background(), cl, op, sharedCluster, true)); !sameNames(got, []string{"payments/shared"}) {
		t.Fatalf("operator cluster databases %#v", got)
	}
	if got := requestNames(mapByCluster(context.Background(), cl, op, localCluster, false)); !sameNames(got, []string{"payments/local-user"}) {
		t.Fatalf("local cluster users %#v", got)
	}
	if got := requestNames(mapByCluster(context.Background(), cl, op, sharedCluster, false)); !sameNames(got, []string{"payments/default-ref"}) {
		t.Fatalf("operator cluster users %#v", got)
	}
}

func requestNames(requests []reconcile.Request) map[string]struct{} {
	out := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		out[request.Namespace+"/"+request.Name] = struct{}{}
	}
	return out
}

func sameNames(got map[string]struct{}, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			return false
		}
	}
	return true
}
