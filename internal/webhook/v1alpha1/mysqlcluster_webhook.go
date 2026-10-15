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
var mysqlclusterlog = logf.Log.WithName("mysqlcluster-resource")

// SetupMySQLClusterWebhookWithManager registers the webhook for MySQLCluster in the manager.
func SetupMySQLClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &mysqlprovisioningv1alpha1.MySQLCluster{}).
		WithValidator(&MySQLClusterCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-mysql-provisioning-tapclap-com-v1alpha1-mysqlcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=mysql-provisioning.tapclap.com,resources=mysqlclusters,verbs=create;update,versions=v1alpha1,name=vmysqlcluster-v1alpha1.kb.io,admissionReviewVersions=v1

// MySQLClusterCustomValidator struct is responsible for validating the MySQLCluster resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type MySQLClusterCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type MySQLCluster.
func (v *MySQLClusterCustomValidator) ValidateCreate(_ context.Context, obj *mysqlprovisioningv1alpha1.MySQLCluster) (admission.Warnings, error) {
	mysqlclusterlog.Info("validation for MySQLCluster upon creation", "name", obj.GetName())
	return nil, validate.Cluster(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type MySQLCluster.
func (v *MySQLClusterCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *mysqlprovisioningv1alpha1.MySQLCluster) (admission.Warnings, error) {
	mysqlclusterlog.Info("validation for MySQLCluster upon update", "name", newObj.GetName())
	return nil, validate.Cluster(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type MySQLCluster.
func (v *MySQLClusterCustomValidator) ValidateDelete(_ context.Context, obj *mysqlprovisioningv1alpha1.MySQLCluster) (admission.Warnings, error) {
	mysqlclusterlog.Info("Validation for MySQLCluster upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
