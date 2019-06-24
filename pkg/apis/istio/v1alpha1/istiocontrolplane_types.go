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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HelmValuesType is typedef for Helm .Values
type HelmValuesType map[string]interface{}

// IstioControlPlaneSpec defines the desired state of IstioControlPlane
// +k8s:openapi-gen=true
type IstioControlPlaneSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html

	// ChartPath represents the relative or absolute path to the charts defining the control plane.
	// Relative paths will be evaluated with the --base-chart-path argument.
	// Empty paths will use the --default-chart-path argument.
	ChartPath string `json:"chartPath,omitempty"`

	// RawValues represents "raw" values.yaml data.
	RawValues HelmValuesType `json:"rawValues,omitempty"`
}

// IstioControlPlaneStatus defines the observed state of IstioControlPlane
// +k8s:openapi-gen=true
type IstioControlPlaneStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html

	// ObservedGeneration represents the last generation processed by the controller.  This is used to short-circuit reconcilations.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the condition of the control plane
	Conditions []Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IstioControlPlane is the Schema for the istiocontrolplanes API
// +k8s:openapi-gen=true
type IstioControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IstioControlPlaneSpec   `json:"spec,omitempty"`
	Status IstioControlPlaneStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IstioControlPlaneList contains a list of IstioControlPlane
type IstioControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IstioControlPlane `json:"items"`
}

// ConditionType represents the type of the condition.  Condition stages are:
// Initialized, Installed, Reconciled
type ConditionType string

const (
	// ConditionTypeInitialized signifies the whether or not the controller has
	// initialized the CR.
	ConditionTypeInitialized ConditionType = "Initialized"
	// ConditionTypeInstalled signifies the whether or not the controller has
	// installed the resources defined through the CR.
	ConditionTypeInstalled ConditionType = "Installed"
	// ConditionTypeReconciled signifies the whether or not the controller has
	// reconciled the resources defined through the CR.
	ConditionTypeReconciled ConditionType = "Reconciled"
)

// ConditionStatus represents the status of the condition
type ConditionStatus string

const (
	// ConditionStatusTrue represents completion of the condition, e.g.
	// Initialized=True signifies that initialization has occurred.
	ConditionStatusTrue ConditionStatus = "True"
	// ConditionStatusFalse represents incomplete status of the condition, e.g.
	// Initialized=False signifies that initialization has not occurred or has
	// failed.
	ConditionStatusFalse ConditionStatus = "False"
	// ConditionStatusUnknown represents unknown completion of the condition, e.g.
	// Initialized=Unknown signifies that initialization may or may not have been
	// completed.
	ConditionStatusUnknown ConditionStatus = "Unknown"
)

// ConditionReason represents a short message indicating how the condition came
// to be in its present state.
type ConditionReason string

const (
	// ConditionReasonInstallSuccessful ...
	ConditionReasonInstallSuccessful ConditionReason = "InstallSuccessful"
	// ConditionReasonInstallError ...
	ConditionReasonInstallError ConditionReason = "InstallError"
	// ConditionReasonReconcileSuccessful ...
	ConditionReasonReconcileSuccessful ConditionReason = "ReconcileSuccessful"
	// ConditionReasonReconcileError ...
	ConditionReasonReconcileError ConditionReason = "ReconcileError"
)

// Condition represents a specific condition on a resource
type Condition struct {
	Type               ConditionType   `json:"type,omitempty"`
	Status             ConditionStatus `json:"status,omitempty"`
	Reason             ConditionReason `json:"reason,omitempty"`
	Message            string          `json:"message,omitempty"`
	LastTransitionTime metav1.Time     `json:"lastTransitionTime,omitempty"`
}

// GetCondition returns a condition for the list of conditions
func (s *IstioControlPlaneStatus) GetCondition(conditionType ConditionType) Condition {
	if s == nil {
		return Condition{Type: conditionType, Status: ConditionStatusUnknown}
	}
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			return s.Conditions[i]
		}
	}
	return Condition{Type: conditionType, Status: ConditionStatusUnknown}
}

// SetCondition sets a specific condition in the list of conditions
func (s *IstioControlPlaneStatus) SetCondition(condition Condition) *IstioControlPlaneStatus {
	if s == nil {
		return nil
	}
	now := metav1.Now()
	for i := range s.Conditions {
		if s.Conditions[i].Type == condition.Type {
			if s.Conditions[i].Status != condition.Status {
				condition.LastTransitionTime = now
			} else {
				condition.LastTransitionTime = s.Conditions[i].LastTransitionTime
			}
			s.Conditions[i] = condition
			return s
		}
	}

	// If the condition does not exist,
	// initialize the lastTransitionTime
	condition.LastTransitionTime = now
	s.Conditions = append(s.Conditions, condition)
	return s
}

// RemoveCondition removes a condition for the list of conditions
func (s *IstioControlPlaneStatus) RemoveCondition(conditionType ConditionType) *IstioControlPlaneStatus {
	if s == nil {
		return nil
	}
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			s.Conditions = append(s.Conditions[:i], s.Conditions[i+1:]...)
			return s
		}
	}
	return s
}

func init() {
	SchemeBuilder.Register(&IstioControlPlane{}, &IstioControlPlaneList{})
}
