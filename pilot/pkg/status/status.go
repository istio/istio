// Copyright Istio Authors
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

package status

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IstioStatus struct {
	Conditions []IstioCondition `json:"conditions"`
	// TODO: Messages should be typed for ease of use.
	ValidationMessages []interface{} `json:"validationMessages"`
}

// IstioConditionType is a valid value for IstioCondition.Type
type IstioConditionType string

// These are valid conditions of pod.
const (
	// Reconciled indicates whether this version of the resource has reached all dataplane instances or not.
	Reconciled IstioConditionType = "Reconciled"
	// PassedValidation indicates whether background analysis found any problems with this config
	PassedValidation IstioConditionType = "PassedValidation"
)

// IstioCondition contains details for the current condition of this pod.
type IstioCondition struct {
	// Type is the type of the condition.
	Type IstioConditionType `json:"type"`
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`
	// Last time we probed the condition.
	// +optional
	LastProbeTime v1.Time `json:"lastProbeTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime v1.Time `json:"lastTransitionTime,omitempty"`
	// Unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}
