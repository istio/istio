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

package ambient

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gtwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gtwapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1" // TODO: should likely update to v1 but this is the type currently recognized byt kclient

	"istio.io/api/label"
	"istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	apiv1beta1 "istio.io/api/type/v1beta1"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/workloadapi"
)

const (
	rootns string = "test-system"
)

func TestConvertAuthorizationPolicyStatus(t *testing.T) {
	testCases := []struct {
		name                string
		inputAuthzPol       *securityclient.AuthorizationPolicy
		expectStatusMessage *model.StatusMessage
	}{
		{
			name: "no status - targetRef is not for ztunnel",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					TargetRef: &apiv1beta1.PolicyTargetReference{
						Kind: "Service",
						Name: "test-svc",
					},
					Rules: []*v1beta1.Rule{},
				},
			},
			expectStatusMessage: nil,
		},
		{
			name: "no status - policy is fine",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										Principals: []string{
											"trust/ns/echo-1234/",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Ports: []string{
											"http",
										},
									},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: nil,
		},
		{
			name: "unsupported AUDIT action",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Action: v1beta1.AuthorizationPolicy_AUDIT,
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										Principals: []string{
											"trust/ns/echo-1234/",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Ports: []string{
											"http",
										},
									},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: &model.StatusMessage{
				Reason:  "UnsupportedValue",
				Message: "ztunnel does not support the AUDIT action",
			},
		},
		{
			name: "unsupported CUSTOM action",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Action: v1beta1.AuthorizationPolicy_CUSTOM,
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										Principals: []string{
											"trust/ns/echo-1234/",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Ports: []string{
											"http",
										},
									},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: &model.StatusMessage{
				Reason:  "UnsupportedValue",
				Message: "ztunnel does not support the CUSTOM action",
			},
		},
		{
			name: "unsupported HTTP methods",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										RequestPrincipals: []string{
											"example.com/sub-1",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Methods: []string{
											"GET",
										},
									},
								},
							},
							When: []*v1beta1.Condition{
								{
									Key:    "request.auth.presenter",
									Values: []string{"presenter.example.com"},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: &model.StatusMessage{
				Reason: "UnsupportedValue",
				Message: "ztunnel does not support HTTP attributes (found: methods, request.auth.presenter, requestPrincipals). " +
					"In ambient mode you must use a waypoint proxy to enforce HTTP rules. " +
					"Within an ALLOW policy, rules matching HTTP attributes are omitted. " +
					"This will be more restrictive than requested.",
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			_, outStatusMessage := convertAuthorizationPolicy(rootns, test.inputAuthzPol)
			assert.Equal(t, test.expectStatusMessage, outStatusMessage)
		})
	}
}

func TestWaypointPolicyStatusCollection(t *testing.T) {
	stop := test.NewStop(t)
	opts := krt.NewOptionsBuilder(stop, "", krt.GlobalDebugHandler)
	c := kube.NewFakeClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientAuthzPol := kclient.New[*securityclient.AuthorizationPolicy](c)
	authzPolCol := krt.WrapClient(clientAuthzPol, opts.WithName("authzPolCol")...)

	clientSvc := kclient.New[*v1.Service](c)
	svcCol := krt.WrapClient(clientSvc, opts.WithName("svcCol")...)

	clientSe := kclient.New[*networkingclient.ServiceEntry](c)
	seCol := krt.WrapClient(clientSe, opts.WithName("seCol")...)

	clientGwClass := kclient.New[*gtwapiv1beta1.GatewayClass](c)
	gwClassCol := krt.WrapClient(clientGwClass, opts.WithName("gwClassCol")...)

	meshConfigMock := krttest.NewMock(t, []any{
		meshwatcher.MeshConfigResource{
			MeshConfig: mesh.DefaultMeshConfig(),
		},
	})
	meshConfigCol := GetMeshConfig(meshConfigMock)

	clientNs := kclient.New[*v1.Namespace](c)
	nsCol := krt.WrapClient(clientNs, opts.WithName("nsCol")...)

	clientGtw := kclient.New[*gtwapiv1beta1.Gateway](c)
	gtwCol := krt.WrapClient(clientGtw, opts.WithName("gtwCol")...)
	waypointCol := krt.NewCollection(gtwCol, func(ctx krt.HandlerContext, i *gtwapiv1beta1.Gateway) *Waypoint {
		if i == nil {
			return nil
		}
		if len(i.Status.Addresses) == 0 {
			return nil
		}
		// make a very, very simple waypoint
		return &Waypoint{
			Named: krt.Named{
				Name:      "waypoint",
				Namespace: testNS,
			},
			Address: &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Hostname{
					Hostname: &workloadapi.NamespacedHostname{
						Namespace: i.Namespace,
						Hostname:  i.Status.Addresses[0].Value,
					},
				},
			},
			TrafficType: constants.ServiceTraffic,
		}
	}, opts.WithName("waypoint")...)

	wpsCollection := WaypointPolicyStatusCollection(authzPolCol, waypointCol, svcCol, seCol, gwClassCol, meshConfigCol, nsCol, opts)
	c.RunAndWait(ctx.Done())

	_, err := clientNs.Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testNS,
			Labels: map[string]string{label.IoIstioUseWaypoint.Name: "waypoint"},
		},
	})
	assert.NoError(t, err)

	addrType := gtwapiv1.HostnameAddressType
	_, err = clientGtw.Create(&gtwapiv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "waypoint",
			Namespace: testNS,
		},
		Spec: gtwapiv1.GatewaySpec{},
		Status: gtwapiv1.GatewayStatus{
			Addresses: []gtwapiv1.GatewayStatusAddress{
				{
					Type:  &addrType,
					Value: "waypoint.default.cluster.local",
				},
			},
		},
	})
	assert.NoError(t, err)

	testCases := []TestWaypointPolicyStatusCollectionTestCase{
		{
			testName: "single-bind-success-serviceentry",
			serviceEntries: []networkingclient.ServiceEntry{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "working-se",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "working-se-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "working-se",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/working-se",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-success-serviceentry-targetRef",
			serviceEntries: []networkingclient.ServiceEntry{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "working-se-tr",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "working-se-pol-tr",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRef: &apiv1beta1.PolicyTargetReference{
						Group: gvk.ServiceEntry.Group,
						Kind:  gvk.ServiceEntry.Kind,
						Name:  "working-se-tr",
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/working-se-tr",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-no-waypoint-serviceentry",
			serviceEntries: []networkingclient.ServiceEntry{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-waypoint-se",
						Namespace: testNS,
						Labels: map[string]string{
							label.IoIstioUseWaypoint.Name: "none",
						},
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "no-waypoint-se-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "no-waypoint-se",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/no-waypoint-se",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAncestorNotBound,
						Message: gvk.ServiceEntry.Kind + " " + testNS + "/no-waypoint-se is not bound to a waypoint",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName:       "single-bind-missing-ref-serviceentry",
			serviceEntries: []networkingclient.ServiceEntry{},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "missing-se-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "missing-se",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/missing-se",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: gvk.ServiceEntry.Kind + " " + testNS + "/missing-se was not found",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "multi-bind-success-serviceentry",
			serviceEntries: []networkingclient.ServiceEntry{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "multi-working-se-1",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "multi-working-se-2",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "multi-working-se-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "multi-working-se-1",
						},
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "multi-working-se-2",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/multi-working-se-1",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/multi-working-se-2",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "multi-partial-bind-serviceentry",
			serviceEntries: []networkingclient.ServiceEntry{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-partial-no-waypoint-se-1",
						Namespace: testNS,
						Labels: map[string]string{
							label.IoIstioUseWaypoint.Name: "none",
						},
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "multi-partial-bound-se-1",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1alpha3.ServiceEntry{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "multi-partial-se-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "multi-partial-no-waypoint-se-1",
						},
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "multi-partial-bound-se-1",
						},
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "multi-partial-missing-se-1",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/multi-partial-no-waypoint-se-1",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAncestorNotBound,
						Message: gvk.ServiceEntry.Kind + " " + testNS + "/multi-partial-no-waypoint-se-1 is not bound to a waypoint",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/multi-partial-bound-se-1",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/multi-partial-missing-se-1",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: gvk.ServiceEntry.Kind + " " + testNS + "/multi-partial-missing-se-1 was not found",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-success-service",
			services: []v1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "working-service",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1.ServiceSpec{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "working-service-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.Service.Group,
							Kind:  gvk.Service.Kind,
							Name:  "working-service",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Service.core:ns1/working-service",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-success-service-targetRef",
			services: []v1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "working-service-tr",
						Namespace:  testNS,
						Generation: 1,
					},
					Spec: v1.ServiceSpec{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "working-service-pol-tr",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRef: &apiv1beta1.PolicyTargetReference{
						Group: gvk.Service.Group,
						Kind:  gvk.Service.Kind,
						Name:  "working-service-tr",
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Service.core:ns1/working-service-tr",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-no-waypoint-service",
			services: []v1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-waypoint-service",
						Namespace: testNS,
						Labels: map[string]string{
							label.IoIstioUseWaypoint.Name: "none",
						},
						Generation: 1,
					},
					Spec: v1.ServiceSpec{},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "no-waypoint-service-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.Service.Group,
							Kind:  gvk.Service.Kind,
							Name:  "no-waypoint-service",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Service.core:ns1/no-waypoint-service",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAncestorNotBound,
						Message: "Service " + testNS + "/no-waypoint-service is not bound to a waypoint",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-no-service",
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "no-service-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.Service.Group,
							Kind:  gvk.Service.Kind,
							Name:  "no-service",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Service.core:ns1/no-service",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: "Service " + testNS + "/no-service was not found",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-success-gateway",
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "single-gateway-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.KubernetesGateway.Group,
							Kind:  gvk.KubernetesGateway.Kind,
							Name:  "waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Gateway.gateway.networking.k8s.io:ns1/waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-success-gateway-targetRef",
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "single-gateway-pol-tr",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.KubernetesGateway.Group,
							Kind:  gvk.KubernetesGateway.Kind,
							Name:  "waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Gateway.gateway.networking.k8s.io:ns1/waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-no-gateway",
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "single-no-gateway-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.KubernetesGateway.Group,
							Kind:  gvk.KubernetesGateway.Kind,
							Name:  "not-a-waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "Gateway.gateway.networking.k8s.io:ns1/not-a-waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: "not bound",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "multi-bind-partial-all-resources",
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "multi-partial-all-pol",
					Namespace:  testNS,
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "working-se",
						},
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "no-waypoint-se",
						},
						{
							Group: gvk.ServiceEntry.Group,
							Kind:  gvk.ServiceEntry.Kind,
							Name:  "missing-se",
						},
						{
							Group: gvk.Service.Group,
							Kind:  gvk.Service.Kind,
							Name:  "working-service",
						},
						{
							Group: gvk.Service.Group,
							Kind:  gvk.Service.Kind,
							Name:  "no-waypoint-service",
						},
						{
							Group: gvk.Service.Group,
							Kind:  gvk.Service.Kind,
							Name:  "no-service",
						},
						{
							Group: gvk.KubernetesGateway.Group,
							Kind:  gvk.KubernetesGateway.Kind,
							Name:  "waypoint",
						},
						{
							Group: gvk.KubernetesGateway.Group,
							Kind:  gvk.KubernetesGateway.Kind,
							Name:  "not-a-waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/working-se",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/no-waypoint-se",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAncestorNotBound,
						Message: gvk.ServiceEntry.Kind + " " + testNS + "/no-waypoint-se is not bound to a waypoint",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "ServiceEntry.networking.istio.io:ns1/missing-se",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: gvk.ServiceEntry.Kind + " " + testNS + "/missing-se was not found",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "Service.core:ns1/working-service",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "Service.core:ns1/no-waypoint-service",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAncestorNotBound,
						Message: "Service " + testNS + "/no-waypoint-service is not bound to a waypoint",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "Service.core:ns1/no-service",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: "Service " + testNS + "/no-service was not found",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "Gateway.gateway.networking.k8s.io:ns1/waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to " + testNS + "/waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
				{
					Ancestor: "Gateway.gateway.networking.k8s.io:ns1/not-a-waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: "not bound",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "single-bind-gateway-class",
			gatewayClasses: []gtwapiv1beta1.GatewayClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "istio-waypoint",
					},
					Spec: gtwapiv1beta1.GatewayClassSpec{
						ControllerName: constants.ManagedGatewayMeshController,
					},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "single-gateway-class-pol",
					Namespace:  "istio-system",
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.GatewayClass.Group,
							Kind:  gvk.GatewayClass.Kind,
							Name:  "istio-waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "GatewayClass.gateway.networking.k8s.io:istio-system/istio-waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonAccepted,
						Message: "bound to istio-waypoint",
					},
					Bound:              true,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName:       "nonexistent-gateway-class",
			gatewayClasses: []gtwapiv1beta1.GatewayClass{},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "single-no-gateway-class-pol",
					Namespace:  "istio-system",
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.GatewayClass.Group,
							Kind:  gvk.GatewayClass.Kind,
							Name:  "nonexistent-gateway-class",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "GatewayClass.gateway.networking.k8s.io:istio-system/nonexistent-gateway-class",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonTargetNotFound,
						Message: "not bound",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "non-waypoint-gateway-class",
			gatewayClasses: []gtwapiv1beta1.GatewayClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-for-waypoint",
					},
					Spec: gtwapiv1beta1.GatewayClassSpec{
						ControllerName: "random-controller",
					},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "non-waypoint-gateway-class-pol",
					Namespace:  "istio-system",
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.GatewayClass.Group,
							Kind:  gvk.GatewayClass.Kind,
							Name:  "not-for-waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "GatewayClass.gateway.networking.k8s.io:istio-system/not-for-waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonInvalid,
						Message: fmt.Sprintf("GatewayClass must use controller name `%s` for waypoints", constants.ManagedGatewayMeshController),
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
		{
			testName: "gateway-class-ap-not-in-root-ns",
			gatewayClasses: []gtwapiv1beta1.GatewayClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "waypoint",
					},
					Spec: gtwapiv1beta1.GatewayClassSpec{
						ControllerName: constants.ManagedGatewayMeshController,
					},
				},
			},
			policy: securityclient.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "gateway-class-ap-not-in-root-ns-pol",
					Namespace:  "other-ns",
					Generation: 1,
				},
				Spec: v1beta1.AuthorizationPolicy{
					TargetRefs: []*apiv1beta1.PolicyTargetReference{
						{
							Group: gvk.GatewayClass.Group,
							Kind:  gvk.GatewayClass.Kind,
							Name:  "waypoint",
						},
					},
					Rules:  []*v1beta1.Rule{},
					Action: 0,
				},
			},
			expect: []model.PolicyBindingStatus{
				{
					Ancestor: "GatewayClass.gateway.networking.k8s.io:other-ns/waypoint",
					Status: &model.StatusMessage{
						Reason:  model.WaypointPolicyReasonInvalid,
						Message: "AuthorizationPolicy must be in the root namespace `istio-system` when referencing a GatewayClass",
					},
					Bound:              false,
					ObservedGeneration: 1,
				},
			},
		},
	}

	// these nolint are to suppress findings regarding copying the mutex contained within our service entry proto fields

	// nolint: govet
	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			// nolint: govet
			for _, se := range tc.serviceEntries {
				_, err := clientSe.Create(&se)
				assert.NoError(t, err)
			}

			for _, s := range tc.services {
				_, err := clientSvc.Create(&s)
				assert.NoError(t, err)
			}

			for _, gwClass := range tc.gatewayClasses {
				_, err := clientGwClass.Create(&gwClass)
				assert.NoError(t, err)
			}

			_, err := clientAuthzPol.Create(&tc.policy)
			assert.NoError(t, err)

			assert.EventuallyEqual(t, func() []model.PolicyBindingStatus {
				s := getStatus(wpsCollection, tc.policy.Name, tc.policy.Namespace)
				if s == nil {
					return nil
				}
				return s.Conditions
			}, tc.expect, retry.Timeout(2*time.Second))
		})
	}
}

type TestWaypointPolicyStatusCollectionTestCase struct {
	testName       string
	serviceEntries []networkingclient.ServiceEntry
	services       []v1.Service
	gatewayClasses []gtwapiv1beta1.GatewayClass
	policy         securityclient.AuthorizationPolicy
	expect         []model.PolicyBindingStatus
}

func getStatus[T any](col krt.Collection[T], name, namespace string) *T {
	return col.GetKey(namespace + "/" + name)
}
