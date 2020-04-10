package status

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)
import v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
