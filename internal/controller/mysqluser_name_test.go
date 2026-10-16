package controller

import (
	"strings"
	"testing"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

func TestRandomSuffix(t *testing.T) {
	got, err := randomSuffix(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("suffix length %d", len(got))
	}
	for _, r := range got {
		if !strings.ContainsRune(usernameAlphabet, r) {
			t.Fatalf("suffix character %q", r)
		}
	}
}

func TestAccountNameUsesRecordedName(t *testing.T) {
	user := &mysqlv1alpha1.MySQLUser{}
	user.Spec.Username = "user"
	if accountName(user) != "user" {
		t.Fatalf("account name %q", accountName(user))
	}
	user.Status.Username = "usera1b2c3d4"
	if accountName(user) != "usera1b2c3d4" {
		t.Fatalf("recorded account name %q", accountName(user))
	}
}
