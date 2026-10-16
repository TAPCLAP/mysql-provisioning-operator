package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/password"
	"github.com/tapclap/mysql-provisioning-operator/internal/secrettpl"
)

type appSecret struct {
	found         bool
	password      string
	previous      string
	retainedUntil time.Time
	resource      *corev1.Secret
}

func (r *MySQLUserReconciler) readAppSecret(ctx context.Context, user *mysqlv1alpha1.MySQLUser) (appSecret, error) {
	var secret corev1.Secret
	err := r.access().reader().Get(ctx, types.NamespacedName{Namespace: user.Namespace, Name: user.Spec.Secret.Name}, &secret)
	if apierrors.IsNotFound(err) {
		return appSecret{}, nil
	}
	if err != nil {
		return appSecret{}, err
	}
	out := appSecret{
		found:    true,
		password: string(secret.Data[mysqlv1alpha1.SecretKeyPassword]),
		previous: string(secret.Data[mysqlv1alpha1.SecretKeyPasswordPrevious]),
		resource: secret.DeepCopy(),
	}
	if raw := secret.Annotations[mysqlv1alpha1.AnnotationPreviousDiscardAfter]; raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			out.retainedUntil = parsed
		}
	}
	return out, nil
}

func (r *MySQLUserReconciler) writeAppSecret(ctx context.Context, user *mysqlv1alpha1.MySQLUser, rendered map[string]string, hash string, retainedUntil time.Time) error {
	key := types.NamespacedName{Namespace: user.Namespace, Name: user.Spec.Secret.Name}
	var secret corev1.Secret
	err := r.access().reader().Get(ctx, key, &secret)
	create := apierrors.IsNotFound(err)
	if err != nil && !create {
		return err
	}
	renderedData := make(map[string][]byte, len(rendered))
	for name, value := range rendered {
		renderedData[name] = []byte(value)
	}
	discard := ""
	if !retainedUntil.IsZero() {
		discard = retainedUntil.UTC().Format(time.RFC3339)
	}
	if !create && secretMatches(&secret, renderedData, hash, discard) {
		return nil
	}
	if create {
		secret = corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	}
	if err := controllerutil.SetControllerReference(user, &secret, r.Scheme); err != nil {
		return err
	}
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	secret.Labels[mysqlv1alpha1.ManagedByLabel] = mysqlv1alpha1.ManagedByValue
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[mysqlv1alpha1.AnnotationPasswordHash] = hash
	if retainedUntil.IsZero() {
		delete(secret.Annotations, mysqlv1alpha1.AnnotationPreviousDiscardAfter)
	} else {
		secret.Annotations[mysqlv1alpha1.AnnotationPreviousDiscardAfter] = retainedUntil.UTC().Format(time.RFC3339)
	}
	secret.Type = corev1.SecretTypeOpaque
	secret.Data = renderedData
	if create {
		return r.Create(ctx, &secret)
	}
	return r.Update(ctx, &secret)
}

func secretMatches(secret *corev1.Secret, data map[string][]byte, hash, discard string) bool {
	if secret.Annotations[mysqlv1alpha1.AnnotationPasswordHash] != hash {
		return false
	}
	if secret.Annotations[mysqlv1alpha1.AnnotationPreviousDiscardAfter] != discard {
		return false
	}
	if len(secret.Data) != len(data) {
		return false
	}
	for key, value := range data {
		if string(secret.Data[key]) != string(value) {
			return false
		}
	}
	return true
}

func (r *MySQLUserReconciler) deleteAppSecret(ctx context.Context, user *mysqlv1alpha1.MySQLUser) error {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: user.Spec.Secret.Name, Namespace: user.Namespace}}
	return ignoreNotFound(r.Delete(ctx, secret))
}

func renderSecret(user *mysqlv1alpha1.MySQLUser, target resolved, plan password.Output, previous string) (map[string]string, error) {
	data := secrettpl.Data{
		Username:         accountName(user),
		Password:         plan.Password,
		PreviousPassword: previous,
		Hosts:            effectiveHosts(user),
		Plugin:           string(user.Spec.AuthPlugin),
		Cluster:          user.Spec.Cluster,
		Members:          memberEndpoints(target),
		Master:           masterEndpoint(target),
		Databases:        grantDatabases(user),
	}
	entries := make([]secrettpl.Entry, 0, len(user.Spec.Secret.Entries))
	for _, entry := range user.Spec.Secret.Entries {
		entries = append(entries, secrettpl.Entry{ForEach: entry.ForEach, Key: entry.Key, Value: entry.Value})
	}
	rendered, err := secrettpl.Render(user.Spec.Secret.Data, entries, data)
	if err != nil {
		return nil, err
	}
	delete(rendered, mysqlv1alpha1.SecretKeyPassword)
	delete(rendered, mysqlv1alpha1.SecretKeyPasswordPrevious)
	rendered[mysqlv1alpha1.SecretKeyPassword] = plan.Password
	if previous != "" {
		rendered[mysqlv1alpha1.SecretKeyPasswordPrevious] = previous
	}
	return rendered, nil
}

func memberEndpoints(target resolved) []secrettpl.Endpoint {
	out := make([]secrettpl.Endpoint, 0, len(target.Cluster.Spec.Members))
	for i, member := range target.Cluster.Spec.Members {
		out = append(out, secrettpl.Endpoint{Name: member.Name, Host: member.Host, Port: member.AddressPort(), Index: i + 1})
	}
	return out
}

func masterEndpoint(target resolved) secrettpl.Endpoint {
	if target.Selection.Master == nil {
		return secrettpl.Endpoint{}
	}
	master := target.Selection.Master
	index := 0
	if target.Cluster != nil {
		for i, member := range target.Cluster.Spec.Members {
			if member.Name == master.Name {
				index = i + 1
				break
			}
		}
	}
	return secrettpl.Endpoint{Name: master.Name, Host: master.Host, Port: master.AddressPort(), Index: index}
}

func grantDatabases(user *mysqlv1alpha1.MySQLUser) []string {
	out := make([]string, 0, len(user.Spec.Grants))
	for _, grant := range user.Spec.Grants {
		out = append(out, grant.Database)
	}
	return out
}

func effectiveHosts(user *mysqlv1alpha1.MySQLUser) []string {
	if len(user.Spec.Hosts) == 0 {
		return []string{"%"}
	}
	return append([]string(nil), user.Spec.Hosts...)
}

func removedHosts(previous, current []string) []string {
	currentSet := map[string]struct{}{}
	for _, host := range current {
		currentSet[host] = struct{}{}
	}
	var removed []string
	for _, host := range previous {
		if _, ok := currentSet[host]; !ok {
			removed = append(removed, host)
		}
	}
	return removed
}
