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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	mysqlprovisioningv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/validate"
)

// nolint:unused
// log is for logging in this package.
var mysqltenantlog = logf.Log.WithName("mysqltenant-resource")

// SetupMySQLTenantWebhookWithManager registers the webhook for MySQLTenant in the manager.
func SetupMySQLTenantWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &mysqlprovisioningv1alpha1.MySQLTenant{}).
		WithValidator(&MySQLTenantCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-mysql-provisioning-tapclap-com-v1alpha1-mysqltenant,mutating=false,failurePolicy=fail,sideEffects=None,groups=mysql-provisioning.tapclap.com,resources=mysqltenants,verbs=create;update,versions=v1alpha1,name=vmysqltenant-v1alpha1.kb.io,admissionReviewVersions=v1

// MySQLTenantCustomValidator struct is responsible for validating the MySQLTenant resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type MySQLTenantCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type MySQLTenant.
func (v *MySQLTenantCustomValidator) ValidateCreate(_ context.Context, obj *mysqlprovisioningv1alpha1.MySQLTenant) (admission.Warnings, error) {
	mysqltenantlog.Info("validation for MySQLTenant upon creation", "name", obj.GetName())
	return nil, validate.Tenant(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type MySQLTenant.
func (v *MySQLTenantCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *mysqlprovisioningv1alpha1.MySQLTenant) (admission.Warnings, error) {
	mysqltenantlog.Info("validation for MySQLTenant upon update", "name", newObj.GetName())
	return nil, validate.Tenant(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type MySQLTenant.
func (v *MySQLTenantCustomValidator) ValidateDelete(_ context.Context, obj *mysqlprovisioningv1alpha1.MySQLTenant) (admission.Warnings, error) {
	mysqltenantlog.Info("Validation for MySQLTenant upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
