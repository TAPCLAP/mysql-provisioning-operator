package controller

import (
	"context"
	"crypto/rand"
	"fmt"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

const usernameAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// ensureAccountName records the MySQL account name once.
// Without usernameSuffix the name is spec.username.
// With it, a random suffix is appended and then left unchanged.
func (r *MySQLUserReconciler) ensureAccountName(ctx context.Context, user *mysqlv1alpha1.MySQLUser) error {
	if user.Status.Username != "" {
		return nil
	}
	n, err := validate.UsernameSuffixLength(user.Spec.UsernameSuffix)
	if err != nil {
		return err
	}
	name := user.Spec.Username
	if n > 0 {
		suffix, err := randomSuffix(n)
		if err != nil {
			return err
		}
		name += suffix
	}
	if !mysql.ValidIdent(name, 32) {
		return fmt.Errorf("invalid username %q", name)
	}
	user.Status.Username = name
	return updateStatus(ctx, r.Client, user)
}

func accountName(user *mysqlv1alpha1.MySQLUser) string {
	if user.Status.Username != "" {
		return user.Status.Username
	}
	return user.Spec.Username
}

func randomSuffix(n int) (string, error) {
	// 252 is the largest multiple of len(usernameAlphabet) that fits in a byte.
	const limit = 252
	out := make([]byte, n)
	buf := make([]byte, 1)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		if int(buf[0]) >= limit {
			continue
		}
		out[i] = usernameAlphabet[int(buf[0])%len(usernameAlphabet)]
		i++
	}
	return string(out), nil
}
