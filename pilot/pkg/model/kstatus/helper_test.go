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

package kstatus

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"
)

func TestUpdateConditionIfChanged(t *testing.T) {
	original := metav1.Condition{
		Type:               string(k8s.RouteConditionResolvedRefs),
		Reason:             string(k8s.RouteReasonResolvedRefs),
		Status:             StatusTrue,
		Message:            "All references resolved",
		LastTransitionTime: metav1.Now(),
	}
	transitionTime := metav1.NewTime(original.LastTransitionTime.Add(1 * time.Second))
	statusChanged := metav1.Condition{
		Type:               string(k8s.RouteConditionResolvedRefs),
		Reason:             string(k8s.RouteReasonResolvedRefs),
		Status:             StatusFalse,
		Message:            "invalid backend",
		LastTransitionTime: transitionTime,
	}
	messageChanged := metav1.Condition{
		Type:               string(k8s.RouteConditionResolvedRefs),
		Reason:             string(k8s.RouteReasonResolvedRefs),
		Status:             StatusTrue,
		Message:            "foo",
		LastTransitionTime: transitionTime,
	}
	anotherType := metav1.Condition{
		Type:               string(k8s.RouteConditionAccepted),
		Reason:             string(k8s.RouteReasonAccepted),
		Status:             StatusTrue,
		Message:            "Route was valid",
		LastTransitionTime: transitionTime,
	}

	tests := []struct {
		name       string
		conditions []metav1.Condition
		condition  metav1.Condition
		want       []metav1.Condition
	}{
		{
			name:       "unchanged",
			conditions: []metav1.Condition{original},
			condition: func() metav1.Condition {
				c := original
				c.LastTransitionTime = transitionTime
				return c
			}(),
			want: []metav1.Condition{original},
		},
		{
			name:       "status changed",
			conditions: []metav1.Condition{original},
			condition:  statusChanged,
			want:       []metav1.Condition{statusChanged},
		},
		{
			name:       "message changed",
			conditions: []metav1.Condition{original},
			condition:  messageChanged,
			want: []metav1.Condition{func() metav1.Condition {
				c := messageChanged
				c.LastTransitionTime = original.LastTransitionTime
				return c
			}()},
		},
		{
			name:       "another type",
			conditions: []metav1.Condition{original},
			condition:  anotherType,
			want:       []metav1.Condition{original, anotherType},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpdateConditionIfChanged(tt.conditions, tt.condition); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UpdateConditionIfChanged got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCondition(t *testing.T) {
	transitionTime := metav1.Now()

	tests := []struct {
		name       string
		conditions []metav1.Condition
		condition  string
		want       metav1.Condition
	}{
		{
			name: "ResolvedRefs condition",
			conditions: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
				{
					Type:               string(k8s.RouteConditionResolvedRefs),
					Reason:             string(k8s.RouteReasonResolvedRefs),
					Status:             StatusTrue,
					Message:            "foo",
					LastTransitionTime: transitionTime,
				},
			},
			condition: string(k8s.RouteConditionResolvedRefs),
			want: metav1.Condition{
				Type:               string(k8s.RouteConditionResolvedRefs),
				Reason:             string(k8s.RouteReasonResolvedRefs),
				Status:             StatusTrue,
				Message:            "foo",
				LastTransitionTime: transitionTime,
			},
		},
		{
			name: "Empty condition",
			conditions: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
			},
			condition: "",
			want:      metav1.Condition{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCondition(tt.conditions, tt.condition); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetCondition got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateCondition(t *testing.T) {
	transitionTime := metav1.Now()
	tests := []struct {
		name        string
		conditions  []metav1.Condition
		condition   metav1.Condition
		unsetReason string
		want        []metav1.Condition
	}{
		{
			name: "condition is set, reason is unsetReason",
			conditions: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
			},
			condition: metav1.Condition{
				Type:               string(k8s.RouteConditionAccepted),
				Reason:             string(k8s.RouteReasonAccepted),
				Status:             StatusTrue,
				Message:            "foo",
				LastTransitionTime: transitionTime,
			},
			unsetReason: string(k8s.RouteReasonAccepted),
			want: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusTrue,
					Message:            "foo",
					LastTransitionTime: transitionTime,
				},
			},
		},
		{
			name: "condition is set, reason is not unsetReason",
			conditions: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
			},
			condition: metav1.Condition{
				Type:               string(k8s.RouteConditionAccepted),
				Reason:             string(k8s.RouteReasonAccepted),
				Status:             StatusTrue,
				Message:            "foo",
				LastTransitionTime: transitionTime,
			},
			unsetReason: string(k8s.RouteReasonPending),
			want: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
			},
		},
		{
			name: "add a new condition",
			conditions: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteReasonAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
			},
			condition: metav1.Condition{
				Type:               string(k8s.RouteConditionResolvedRefs),
				Reason:             string(k8s.RouteReasonResolvedRefs),
				Status:             StatusTrue,
				Message:            "foo",
				LastTransitionTime: transitionTime,
			},
			unsetReason: string(k8s.RouteReasonNotAllowedByListeners),
			want: []metav1.Condition{
				{
					Type:               string(k8s.RouteConditionAccepted),
					Reason:             string(k8s.RouteConditionAccepted),
					Status:             StatusFalse,
					Message:            "invalid backend",
					LastTransitionTime: transitionTime,
				},
				{
					Type:               string(k8s.RouteConditionResolvedRefs),
					Reason:             string(k8s.RouteReasonResolvedRefs),
					Status:             StatusTrue,
					Message:            "foo",
					LastTransitionTime: transitionTime,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CreateCondition(tt.conditions, tt.condition, tt.unsetReason); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateCondition got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInvertStatus(t *testing.T) {
	tests := []struct {
		name   string
		status metav1.ConditionStatus
		want   metav1.ConditionStatus
	}{
		{
			name:   "return false",
			status: metav1.ConditionTrue,
			want:   metav1.ConditionFalse,
		},
		{
			name:   "return true",
			status: metav1.ConditionFalse,
			want:   metav1.ConditionTrue,
		},
		{
			name:   "default return false",
			status: metav1.ConditionUnknown,
			want:   metav1.ConditionFalse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InvertStatus(tt.status); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InvertStatus got %v, want %v", got, tt.want)
			}
		})
	}
}
