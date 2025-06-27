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

package gateway

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestCreateRouteStatus(t *testing.T) {
	lastTransitionTime := metav1.Now()
	parentRef := httpRouteSpec.ParentRefs[0]
	parentStatus := []k8s.RouteParentStatus{
		{
			ParentRef:      parentRef,
			ControllerName: k8s.GatewayController("another-gateway-controller"),
			Conditions: []metav1.Condition{
				{Type: "foo", Status: "bar"},
			},
		},
		{
			ParentRef:      parentRef,
			ControllerName: k8s.GatewayController(features.ManagedGatewayController),
			Conditions: []metav1.Condition{
				{
					Type:               string(k8s.RouteReasonAccepted),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: lastTransitionTime,
					Message:            "Route was valid",
				},
				{
					Type:               string(k8s.RouteConditionResolvedRefs),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: lastTransitionTime,
					Message:            "All references resolved",
				},
				{
					Type:               string(RouteConditionResolvedWaypoints),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: lastTransitionTime,
					Message:            "All waypoints resolved",
				},
			},
		},
	}

	httpRoute := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.HTTPRoute,
			Namespace:        "foo",
			Name:             "bar",
			Generation:       1,
		},
		Spec: &httpRouteSpec,
		Status: &k8s.HTTPRouteStatus{
			RouteStatus: k8s.RouteStatus{
				Parents: parentStatus,
			},
		},
	}

	type args struct {
		gateways []RouteParentResult
		obj      config.Config
		current  []k8s.RouteParentStatus
	}
	tests := []struct {
		name      string
		args      args
		wantEqual bool
	}{
		{
			name: "no error",
			args: args{
				gateways: []RouteParentResult{{OriginalReference: parentRef}},
				obj:      httpRoute,
				current:  parentStatus,
			},
			wantEqual: true,
		},
		{
			name: "route status error",
			args: args{
				gateways: []RouteParentResult{{OriginalReference: parentRef, RouteError: &ConfigError{
					Reason: ConfigErrorReason(k8s.RouteReasonRefNotPermitted),
				}}},
				obj:     httpRoute,
				current: parentStatus,
			},
			wantEqual: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := createRouteStatus(tt.args.gateways, "default", tt.args.obj.Generation, tt.args.current)
			equal := reflect.DeepEqual(got, tt.args.current)
			if equal != tt.wantEqual {
				t.Errorf("route status: old: %+v, new: %+v", tt.args.current, got)
			}
		})
	}
}
