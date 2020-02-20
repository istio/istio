// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutils "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"istio.io/istio/operator/pkg/validate"
	"istio.io/pkg/log"
)

func (m *IstioOperator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(m).
		Complete()
}

var _ webhook.Defaulter = &IstioOperator{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (m *IstioOperator) Default() {
	log.Infof("setting default for %s", m.Name)
	// nothing to do fro default
}

var _ webhook.Validator = &IstioOperator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (m *IstioOperator) ValidateCreate() error {
	log.Infof("validate create for %s", m.Name)

	return m.validateIstioOperator()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (m *IstioOperator) ValidateUpdate(old runtime.Object) error {
	log.Infof("validate update for %s", m.Name)

	return m.validateIstioOperator()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (m *IstioOperator) ValidateDelete() error {
	log.Infof("validate delete for %s", m.Name)

	// nothing to do for deletion operation
	return nil
}

// Validate the name and the spec of the IstioOperator 
func (m *IstioOperator) validateIstioOperator() error {
	var allErrs field.ErrorList
	if err := m.validateIstioOperatorName(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := m.validateIstioOperatorSpec(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "install.istio.io", Kind: "IstioOperator"},
		m.Name, allErrs)
}

// Validate the name of the IstioOperator
func (m *IstioOperator) validateIstioOperatorName() *field.Error {
	if len(m.ObjectMeta.Name) > validationutils.DNS1035LabelMaxLength {
		// The job name length is 63 character like all Kubernetes objects
		return field.Invalid(field.NewPath("metadata").Child("name"), m.Name, "must be no more than 63 characters")
	}
	return nil
}

// Validate the the spec of the IstioOperator
func (m *IstioOperator) validateIstioOperatorSpec() *field.Error {
	istioOperatorSpec := m.Spec
	if err := validate.CheckIstioOperatorSpec(istioOperatorSpec, false); err != nil {
		return field.Invalid(field.NewPath("spec"), m.Spec, err.Error())
	}
}

