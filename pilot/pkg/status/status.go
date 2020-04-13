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

package status

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

type IstioStatus struct {
	Conditions         []IstioCondition
	ValidationMessages []diag.Message
}

// IstioConditionType is a valid value for IstioCondition.Type
type IstioConditionType string

// These are valid conditions of pod.
const (
	// StillPropagating indicates whether this version of the resource has reached all dataplane instances or not.
	StillPropagating IstioConditionType = "StillPropagating"
	// HasValidationErrors indicates whether background analysis found any problems with this config
	HasValidationErrors IstioConditionType = "HasValidationErrors"
)

// IstioCondition contains details for the current condition of this pod.
type IstioCondition struct {
	// Type is the type of the condition.
	Type IstioConditionType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=IstioConditionType"`
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	Status v1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=ConditionStatus"`
	// Last time we probed the condition.
	// +optional
	LastProbeTime v12.Time `json:"lastProbeTime,omitempty" protobuf:"bytes,3,opt,name=lastProbeTime"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime v12.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// Unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}
