/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	mysqlprovisioningv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/runtimeconfig"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

// nolint:unused
// log is for logging in this package.
var mysqluserlog = logf.Log.WithName("mysqluser-resource")

// SetupMySQLUserWebhookWithManager registers the webhook for MySQLUser in the manager.
func SetupMySQLUserWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &mysqlprovisioningv1alpha1.MySQLUser{}).
		WithValidator(&MySQLUserCustomValidator{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-mysql-provisioning-tapclap-com-v1alpha1-mysqluser,mutating=false,failurePolicy=fail,sideEffects=None,groups=mysql-provisioning.tapclap.com,resources=mysqlusers,verbs=create;update,versions=v1alpha1,name=vmysqluser-v1alpha1.kb.io,admissionReviewVersions=v1

// MySQLUserCustomValidator struct is responsible for validating the MySQLUser resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type MySQLUserCustomValidator struct {
	Client client.Client
}

func (v *MySQLUserCustomValidator) ValidateCreate(ctx context.Context, obj *mysqlprovisioningv1alpha1.MySQLUser) (admission.Warnings, error) {
	mysqluserlog.Info("validation for MySQLUser upon creation", "name", obj.GetName())
	return nil, validate.User(ctx, v.Client, runtimeconfig.OperatorNamespace, obj, true)
}

func (v *MySQLUserCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *mysqlprovisioningv1alpha1.MySQLUser) (admission.Warnings, error) {
	mysqluserlog.Info("validation for MySQLUser upon update", "name", newObj.GetName())
	return nil, validate.User(ctx, v.Client, runtimeconfig.OperatorNamespace, newObj, false)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type MySQLUser.
func (v *MySQLUserCustomValidator) ValidateDelete(_ context.Context, obj *mysqlprovisioningv1alpha1.MySQLUser) (admission.Warnings, error) {
	mysqluserlog.Info("Validation for MySQLUser upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
