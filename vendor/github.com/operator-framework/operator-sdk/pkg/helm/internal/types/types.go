// Copyright 2018 The Operator-SDK Authors
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

package types

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/helm/pkg/proto/hapi/release"
)

type HelmAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []HelmApp `json:"items"`
}

type HelmApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              HelmAppSpec   `json:"spec"`
	Status            HelmAppStatus `json:"status,omitempty"`
}

type HelmAppSpec map[string]interface{}

type HelmAppConditionType string
type ConditionStatus string
type HelmAppConditionReason string

type HelmAppCondition struct {
	Type    HelmAppConditionType   `json:"type"`
	Status  ConditionStatus        `json:"status"`
	Reason  HelmAppConditionReason `json:"reason,omitempty"`
	Message string                 `json:"message,omitempty"`
	Release *release.Release       `json:"release,omitempty"`

	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

const (
	ConditionInitialized    HelmAppConditionType = "Initialized"
	ConditionDeployed       HelmAppConditionType = "Deployed"
	ConditionReleaseFailed  HelmAppConditionType = "ReleaseFailed"
	ConditionIrreconcilable HelmAppConditionType = "Irreconcilable"

	StatusTrue    ConditionStatus = "True"
	StatusFalse   ConditionStatus = "False"
	StatusUnknown ConditionStatus = "Unknown"

	ReasonInstallSuccessful   HelmAppConditionReason = "InstallSuccessful"
	ReasonUpdateSuccessful    HelmAppConditionReason = "UpdateSuccessful"
	ReasonUninstallSuccessful HelmAppConditionReason = "UninstallSuccessful"
	ReasonInstallError        HelmAppConditionReason = "InstallError"
	ReasonUpdateError         HelmAppConditionReason = "UpdateError"
	ReasonReconcileError      HelmAppConditionReason = "ReconcileError"
	ReasonUninstallError      HelmAppConditionReason = "UninstallError"
)

type HelmAppStatus struct {
	Conditions []HelmAppCondition `json:"conditions"`
}

func (s *HelmAppStatus) ToMap() (map[string]interface{}, error) {
	var out map[string]interface{}
	jsonObj, err := json.Marshal(&s)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(jsonObj, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetCondition sets a condition on the status object. If the condition already
// exists, it will be replaced. SetCondition does not update the resource in
// the cluster.
func (s *HelmAppStatus) SetCondition(condition HelmAppCondition) *HelmAppStatus {
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

// RemoveCondition removes the condition with the passed condition type from
// the status object. If the condition is not already present, the returned
// status object is returned unchanged. RemoveCondition does not update the
// resource in the cluster.
func (s *HelmAppStatus) RemoveCondition(conditionType HelmAppConditionType) *HelmAppStatus {
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			s.Conditions = append(s.Conditions[:i], s.Conditions[i+1:]...)
			return s
		}
	}
	return s
}

// StatusFor safely returns a typed status block from a custom resource.
func StatusFor(cr *unstructured.Unstructured) *HelmAppStatus {
	switch s := cr.Object["status"].(type) {
	case *HelmAppStatus:
		return s
	case map[string]interface{}:
		var status *HelmAppStatus
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(s, &status); err != nil {
			return &HelmAppStatus{}
		}
		return status
	default:
		return &HelmAppStatus{}
	}
}
