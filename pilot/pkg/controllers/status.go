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

package controllers

import (
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"
	k8salpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayalpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

type ConfigErrorReason = string

type Condition struct {
	// Reason defines the Reason to report on success. Ignored if error is set
	Reason string
	// Message defines the Message to report on success. Ignored if error is set
	Message string
	// Status defines the Status to report on success. The inverse will be set if error is set
	// If not set, will default to StatusTrue
	Status metav1.ConditionStatus
	// Error defines an Error state; the reason and message will be replaced with that of the Error and
	// the status inverted
	Error *ConfigError
	// SetOnce, if enabled, will only set the condition if it is not yet present or set to this reason
	SetOnce string
}

type ConfigError struct {
	Reason  ConfigErrorReason
	Message string
}

func GetStatus[I, IS any](spec I) IS {
	switch t := any(spec).(type) {
	case *k8salpha.TCPRoute:
		return any(t.Status).(IS)
	case *k8salpha.TLSRoute:
		return any(t.Status).(IS)
	case *k8sbeta.HTTPRoute:
		return any(t.Status).(IS)
	case *k8s.GRPCRoute:
		return any(t.Status).(IS)
	case *k8sbeta.Gateway:
		return any(t.Status).(IS)
	case *k8sbeta.GatewayClass:
		return any(t.Status).(IS)
	case *gatewayx.XBackendTrafficPolicy:
		return any(t.Status).(IS)
	case *gatewayalpha3.BackendTLSPolicy:
		return any(t.Status).(IS)
	case *gatewayx.XListenerSet:
		return any(t.Status).(IS)
	case *inferencev1.InferencePool:
		return any(t.Status).(IS)
	default:
		log.Fatalf("unknown type %T", t)
		return ptr.Empty[IS]()
	}
}

func InRevision(revision string) func(obj any) bool {
	return func(obj any) bool {
		object := controllers.ExtractObject(obj)
		if object == nil {
			return false
		}
		return config.LabelsInRevision(object.GetLabels(), revision)
	}
}

// SetConditions sets the existingConditions with the new conditions
func SetConditions(generation int64, existingConditions []metav1.Condition, conditions map[string]*Condition) []metav1.Condition {
	// Sort keys for deterministic ordering
	for _, k := range slices.Sort(maps.Keys(conditions)) {
		cond := conditions[k]
		setter := kstatus.UpdateConditionIfChanged
		if cond.SetOnce != "" {
			setter = func(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
				return kstatus.CreateCondition(conditions, condition, cond.SetOnce)
			}
		}
		// A condition can be "negative polarity" (ex: ListenerInvalid) or "positive polarity" (ex:
		// ListenerValid), so in order to determine the status we should set each `condition` defines its
		// default positive status. When there is an error, we will invert that. Example: If we have
		// condition ListenerInvalid, the status will be set to StatusFalse. If an error is reported, it
		// will be inverted to StatusTrue to indicate listeners are invalid. See
		// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
		// for more information
		if cond.Error != nil {
			existingConditions = setter(existingConditions, metav1.Condition{
				Type:               k,
				Status:             kstatus.InvertStatus(cond.Status),
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.Error.Reason,
				Message:            cond.Error.Message,
			})
		} else {
			status := cond.Status
			if status == "" {
				status = kstatus.StatusTrue
			}
			existingConditions = setter(existingConditions, metav1.Condition{
				Type:               k,
				Status:             status,
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.Reason,
				Message:            cond.Message,
			})
		}
	}
	return existingConditions
}
