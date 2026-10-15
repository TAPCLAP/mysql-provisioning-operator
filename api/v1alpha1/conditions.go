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

const (
	// ConditionReady is True when the object is fully applied.
	ConditionReady = "Ready"
	// ConditionApplied is True when the desired statements have been executed.
	ConditionApplied = "Applied"
	// ConditionCharacterSetDrift is True when an existing database charset differs from spec.
	ConditionCharacterSetDrift = "CharacterSetDrift"
	// ConditionGrantsDrift is True when grants could not be converged.
	ConditionGrantsDrift = "GrantsDrift"
	// ConditionSecretSynced is True when the application Secret matches the template.
	ConditionSecretSynced = "SecretSynced"
	// ConditionNoMaster is True when no member satisfies master criteria.
	ConditionNoMaster = "NoMaster"
	// ConditionMasterUnavailable is True when the explicit master cannot be dialed.
	ConditionMasterUnavailable = "MasterUnavailable"
	// ConditionSplitBrain is True when more than one member looks like the master.
	ConditionSplitBrain = "SplitBrain"
	// ConditionPasswordRetentionUnsupported is True when the server cannot keep two passwords.
	ConditionPasswordRetentionUnsupported = "PasswordRetentionUnsupported"
	// ConditionRotationInProgress is True while a previous password is still retained.
	ConditionRotationInProgress = "RotationInProgress"
	// ConditionDegraded is True when at least one required member failed.
	ConditionDegraded = "Degraded"
	// ConditionAuthorized is True when the tenant policy allows the object.
	ConditionAuthorized = "Authorized"

	// UserFinalizer drops MySQL accounts before the MySQLUser object is removed.
	UserFinalizer = "mysql-provisioning.tapclap.com/user-cleanup"

	// AnnotationPasswordHash stores the SHA-256 of the password written by the operator.
	AnnotationPasswordHash = "mysql-provisioning.tapclap.com/password-hash"
	// AnnotationPreviousDiscardAfter is the RFC3339 time when the retained password is discarded.
	AnnotationPreviousDiscardAfter = "mysql-provisioning.tapclap.com/previous-discard-after"

	// SecretKeyPassword is the reserved Secret key that holds the current password.
	SecretKeyPassword = "PASSWORD"
	// SecretKeyPasswordPrevious is the reserved Secret key that holds the retained password.
	SecretKeyPasswordPrevious = "PASSWORD_PREVIOUS"

	// ManagedByLabel marks Secrets written by the operator.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	// ManagedByValue is the managed-by label value.
	ManagedByValue = "mysql-provisioning-operator"

	// DefaultCharacterSet is used when a database omits spec.characterSet.
	DefaultCharacterSet = "utf8mb3"
	// DefaultCollation is used when a database omits spec.collation.
	DefaultCollation = "utf8mb3_general_ci"
	// DefaultPasswordRetention is used when a user omits spec.password.retention.
	DefaultPasswordRetention = "24h"
	// MinPasswordRetention is the shortest allowed retention window.
	MinPasswordRetention = "5m"
)
