package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

func TestReadAdminSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "admin"},
		Data: map[string][]byte{
			"username": []byte("root"),
			"password": []byte("secret"),
			"user":     []byte("operator"),
			"pass":     []byte("other"),
		},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	user, password, err := readAdminSecret(context.Background(), reader, "ns", mysqlv1alpha1.AdminSecretRef{Name: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if user != "root" || password != "secret" {
		t.Fatalf("default keys read %q %q", user, password)
	}

	user, password, err = readAdminSecret(context.Background(), reader, "ns", mysqlv1alpha1.AdminSecretRef{
		Name: "admin", UsernameKey: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user != "operator" || password != "secret" {
		t.Fatalf("username override read %q %q", user, password)
	}

	user, password, err = readAdminSecret(context.Background(), reader, "ns", mysqlv1alpha1.AdminSecretRef{
		Name: "admin", UsernameKey: "user", PasswordKey: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user != "operator" || password != "other" {
		t.Fatalf("custom keys read %q %q", user, password)
	}

	_, _, err = readAdminSecret(context.Background(), reader, "ns", mysqlv1alpha1.AdminSecretRef{
		Name: "admin", PasswordKey: "missing",
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing password key error %v", err)
	}
}
