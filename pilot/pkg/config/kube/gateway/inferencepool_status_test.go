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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

const (
	IstioController = "istio.io/gateway-controller"
	DefaultTestNS   = "default"
	GatewayTestNS   = "gateway-ns"
	AppTestNS       = "app-ns"
	EmptyTestNS     = ""
	infPoolPending  = "Pending"
)

func TestInferencePoolStatusReconciliation(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIInferenceExtension, true)
	testCases := []struct {
		name         string
		givens       []runtime.Object           // Objects to create before the test
		targetPool   *inferencev1.InferencePool // The InferencePool to check
		expectations func(t *testing.T, pool *inferencev1.InferencePoolStatus)
	}{
		//
		// Positive Test Scenarios
		//
		{
			name: "should add gateway parentRef to inferencepool status",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithRouteParentCondition(string(gatewayv1.RouteConditionAccepted), metav1.ConditionTrue, "Accepted", "Accepted"),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, DefaultTestNS, string(status.Parents[0].ParentRef.Namespace))
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1.InferencePoolConditionAccepted),
					Status:  metav1.ConditionTrue,
					Reason:  string(inferencev1.InferencePoolReasonAccepted),
					Message: "Referenced by an HTTPRoute",
				}, "Expected condition with Accepted")
			},
		},
		{
			name: "should add only 1 gateway parentRef to status for multiple routes on different gateways with different controllers",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewGateway("gateway-2", InNamespace(DefaultTestNS), WithGatewayClass("other")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithParentRefAndStatus("gateway-2", DefaultTestNS, "other-controller"),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "gateway-1", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, DefaultTestNS, string(status.Parents[0].ParentRef.Namespace))
			},
		},
		{
			name: "should keep the status of the gateway parentRefs from another controller",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewGateway("gateway-2", InNamespace(DefaultTestNS), WithGatewayClass("other-class")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
				NewHTTPRoute("route-2", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-2", DefaultTestNS, "other-class"),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithParentStatus("gateway-2", DefaultTestNS, WithAcceptedConditions())),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 2, "Expected two parent references")
				assert.ElementsMatch(t,
					[]string{"gateway-1", "gateway-2"},
					[]string{string(status.Parents[0].ParentRef.Name), string(status.Parents[1].ParentRef.Name)},
				)
			},
		},
		{
			name: "should add multiple gateway parentRefs to status for multiple routes",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewGateway("gateway-2", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
				NewHTTPRoute("route-2", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-2", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 2, "Expected two parent references")
				assert.ElementsMatch(t,
					[]string{"gateway-1", "gateway-2"},
					[]string{string(status.Parents[0].ParentRef.Name), string(status.Parents[1].ParentRef.Name)},
				)
			},
		},
		{
			name: "should remove our status from previous reconciliation that is no longer referenced by any HTTPRoute",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewGateway("gateway-2", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS),
				WithParentStatus("gateway-2", DefaultTestNS,
					WithAcceptedConditions(),
				)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "gateway-1", string(status.Parents[0].ParentRef.Name))
			},
		},
		{
			name: "should update/recreate our status from previous reconciliation",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS),
				WithParentStatus("gateway-1", DefaultTestNS,
					WithAcceptedConditions(),
				)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "gateway-1", string(status.Parents[0].ParentRef.Name))
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two conditions")
			},
		},
		{
			name: "should keep others status from previous reconciliation",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewGateway("gateway-2", InNamespace(DefaultTestNS), WithGatewayClass("other-class")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithParentStatus("gateway-2", DefaultTestNS, WithAcceptedConditions())),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 2, "Expected two parent references")
				assert.ElementsMatch(t,
					[]string{"gateway-1", "gateway-2"},
					[]string{string(status.Parents[0].ParentRef.Name), string(status.Parents[1].ParentRef.Name)},
				)
			},
		},
		{
			name: "should remove default parent 'waiting for controller' status",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithParentStatus("default", DefaultTestNS, AsDefaultStatus())),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected two parent references")
				assert.Equal(t, "gateway-1", string(status.Parents[0].ParentRef.Name))
			},
		},
		{
			name: "should remove unknown condition types from controlled parents",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS),
				WithParentStatus("gateway-1", DefaultTestNS,
					WithAcceptedConditions(),
					WithConditions(metav1.ConditionUnknown, "X", "Y", "Dummy"),
				)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected two parent references")
				assert.Equal(t, "gateway-1", string(status.Parents[0].ParentRef.Name))
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two conditions")
				assert.ElementsMatch(t,
					[]string{string(inferencev1.InferencePoolConditionAccepted), string(inferencev1.InferencePoolConditionResolvedRefs)},
					[]string{status.Parents[0].Conditions[0].Type, status.Parents[0].Conditions[1].Type},
				)
			},
		},
		{
			name: "should handle cross-namespace gateway references correctly",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(GatewayTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(AppTestNS),
					WithParentRefAndStatus("main-gateway", GatewayTestNS, IstioController),
					WithBackendRef("test-pool", AppTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(AppTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, GatewayTestNS, string(status.Parents[0].ParentRef.Namespace))
			},
		},
		{
			name: "should handle cross-namespace httproute references correctly",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(GatewayTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(AppTestNS),
					WithParentRefAndStatus("main-gateway", GatewayTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, GatewayTestNS, string(status.Parents[0].ParentRef.Namespace))
			},
		},
		{
			name: "should handle HTTPRoute in same namespace (empty)",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(GatewayTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(AppTestNS),
					WithParentRefAndStatus("main-gateway", GatewayTestNS, IstioController),
					WithBackendRef("test-pool", EmptyTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(AppTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, GatewayTestNS, string(status.Parents[0].ParentRef.Namespace))
			},
		},
		{
			name: "should handle Gateway in same namespace (empty)",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(AppTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(AppTestNS),
					WithParentRefAndStatus("main-gateway", EmptyTestNS, IstioController),
					WithBackendRef("test-pool", AppTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(AppTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, AppTestNS, string(status.Parents[0].ParentRef.Namespace))
			},
		},
		{
			name: "should add only one parentRef for multiple routes on same gateway",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("route-a", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
				NewHTTPRoute("route-b", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected only one parent reference for the same gateway")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
			},
		},
		{
			name: "should report ResolvedRef true when ExtensioNRef found",
			givens: []runtime.Object{
				NewService("test-epp", InNamespace(DefaultTestNS)),
				NewGateway("main-gateway", InNamespace(GatewayTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithExtensionRef("Service", "test-epp")),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two condition")
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1.InferencePoolConditionResolvedRefs),
					Status:  metav1.ConditionTrue,
					Reason:  string(inferencev1.InferencePoolReasonResolvedRefs),
					Message: "Referenced ExtensionRef resolved",
				}, "Expected condition with InvalidExtensionRef")
			},
		},
		{
			name: "should report HTTPRoute not accepted when parent gateway rejects HTTPRoute",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithRouteParentCondition(string(gatewayv1.RouteConditionAccepted), metav1.ConditionFalse, "GatewayNotReady", "Gateway not ready"),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, DefaultTestNS, string(status.Parents[0].ParentRef.Namespace))
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1.InferencePoolConditionAccepted),
					Status:  metav1.ConditionFalse,
					Reason:  string(inferencev1.InferencePoolReasonHTTPRouteNotAccepted),
					Message: "Referenced HTTPRoute default/test-route not accepted by Gateway default/main-gateway",
				}, "Expected condition with HTTPRouteNotAccepted")
			},
		},
		{
			name: "should report unknown status when HTTPRoute parent status has no Accepted condition",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					// Note: No WithRouteParentCondition for Accepted - parent is listed but has no conditions
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", string(status.Parents[0].ParentRef.Name))
				assert.Equal(t, DefaultTestNS, string(status.Parents[0].ParentRef.Namespace))
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1.InferencePoolConditionAccepted),
					Status:  metav1.ConditionUnknown,
					Reason:  string(inferencev1.InferencePoolReasonAccepted),
					Message: "Referenced by an HTTPRoute unknown parentRef Gateway status",
				}, "Expected condition with ConditionUnknown")
			},
		},

		//
		// Negative Test Scenarios
		//
		{
			name: "should not add parentRef for gatewayclass not controlled by us",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(DefaultTestNS), WithGatewayClass("other")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, "other-controller"),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				assert.Empty(t, status.Parents, "ParentRefs should be empty")
			},
		},
		{
			name: "should not add parentRef if httproute has no backendref",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController)), // No BackendRef
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				assert.Empty(t, status.Parents, "ParentRefs should be empty")
			},
		},
		{
			name: "should not add parentRef if httproute has no parentref",
			givens: []runtime.Object{
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithBackendRef("test-pool", DefaultTestNS)), // No ParentRef
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				assert.Empty(t, status.Parents, "ParentRefs should be empty")
			},
		},
		{
			name: "should report ExtensionRef not found if no matching service found",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(GatewayTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two condition")
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1.InferencePoolConditionResolvedRefs),
					Status:  metav1.ConditionFalse,
					Reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
					Message: "Referenced ExtensionRef not found",
				}, "Expected condition with InvalidExtensionRef")
			},
		},
		{
			name: "should report unsupported ExtensionRef if kind is not service",
			givens: []runtime.Object{
				NewGateway("main-gateway", InNamespace(GatewayTestNS), WithGatewayClass("istio")),
				NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("main-gateway", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithExtensionRef("Gateway", "main-gateway")),
			expectations: func(t *testing.T, status *inferencev1.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two condition")
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1.InferencePoolConditionResolvedRefs),
					Status:  metav1.ConditionFalse,
					Reason:  string(inferencev1.InferencePoolReasonInvalidExtensionRef),
					Message: "Unsupported ExtensionRef kind",
				}, "Expected condition with InvalidExtensionRef")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stop := test.NewStop(t)

			controller := setupController(t,
				append(tc.givens, tc.targetPool)...,
			)

			sq := &TestStatusQueue{
				state: map[status.Resource]any{},
			}
			statusSynced := controller.status.SetQueue(sq)
			for _, st := range statusSynced {
				st.WaitUntilSynced(stop)
			}

			dumpOnFailure(t, krt.GlobalDebugHandler)

			getInferencePoolStatus := func() *inferencev1.InferencePoolStatus {
				statuses := sq.Statuses()
				for _, status := range statuses {
					if pool, ok := status.(*inferencev1.InferencePoolStatus); ok {
						return pool
					}
				}
				return nil
			}
			poolStatus := getInferencePoolStatus()
			assert.NotNil(t, poolStatus)

			tc.expectations(t, poolStatus)
		})
	}
}

func assertConditionContains(t *testing.T, conditions []metav1.Condition, expected metav1.Condition, msgAndArgs ...interface{}) {
	t.Helper()

	for _, condition := range conditions {
		if (expected.Type == "" || condition.Type == expected.Type) &&
			(expected.Status == "" || condition.Status == expected.Status) &&
			(expected.Reason == "" || condition.Reason == expected.Reason) &&
			(expected.Message == "" || strings.HasPrefix(condition.Message, expected.Message)) {
			return // Found matching condition
		}
	}

	// If we get here, no matching condition was found
	assert.Fail(t, fmt.Sprintf("Expected condition with Type=%s, Status=%s, Reason=%s not found in conditions. Available conditions: %+v",
		expected.Type, expected.Status, expected.Reason, conditions), msgAndArgs...)
}

// --- Mock Objects ---

// Option is a function that mutates an object.
type Option func(client.Object)

type ParentOption func(*inferencev1.ParentStatus)

// --- Helper functions to mutate objects ---

func InNamespace(namespace string) Option {
	return func(obj client.Object) {
		obj.SetNamespace(namespace)
	}
}

func WithController(name string) Option {
	return func(obj client.Object) {
		gw, ok := obj.(*gateway.GatewayClass)
		if ok {
			gw.Spec.ControllerName = gateway.GatewayController(name)
		}
	}
}

func WithGatewayClass(name string) Option {
	return func(obj client.Object) {
		gw, ok := obj.(*gateway.Gateway)
		if ok {
			gw.Spec.GatewayClassName = gateway.ObjectName(name)
		}
	}
}

func WithParentRef(name, namespace string) Option {
	return func(obj client.Object) {
		hr, ok := obj.(*gateway.HTTPRoute)
		if ok {
			namespaceName := gateway.Namespace(namespace)
			hr.Spec.ParentRefs = []gateway.ParentReference{
				{
					Name:      gateway.ObjectName(name),
					Namespace: &namespaceName,
				},
			}
		}
	}
}

func WithParentRefAndStatus(name, namespace, controllerName string) Option {
	return func(obj client.Object) {
		hr, ok := obj.(*gateway.HTTPRoute)
		if ok {
			namespaceName := gateway.Namespace(namespace)
			if hr.Spec.ParentRefs == nil {
				hr.Spec.ParentRefs = []gateway.ParentReference{}
			}
			hr.Spec.ParentRefs = append(hr.Spec.ParentRefs, gateway.ParentReference{
				Name:      gateway.ObjectName(name),
				Namespace: &namespaceName,
			})
			if hr.Status.Parents == nil {
				hr.Status.Parents = []gateway.RouteParentStatus{}
			}

			parentStatusRef := &gateway.RouteParentStatus{
				ParentRef: gateway.ParentReference{
					Name:      gateway.ObjectName(name),
					Namespace: &namespaceName,
				},
				ControllerName: gateway.GatewayController(controllerName),
			}
			hr.Status.Parents = append(hr.Status.Parents, *parentStatusRef)
		}
	}
}

func WithRouteParentCondition(conditionType string, status metav1.ConditionStatus, reason, message string) Option {
	return func(obj client.Object) {
		hr, ok := obj.(*gateway.HTTPRoute)
		if ok && len(hr.Status.Parents) > 0 {
			// Add condition to the last parent status (most recently added)
			lastParentIdx := len(hr.Status.Parents) - 1
			if hr.Status.Parents[lastParentIdx].Conditions == nil {
				hr.Status.Parents[lastParentIdx].Conditions = []metav1.Condition{}
			}
			hr.Status.Parents[lastParentIdx].Conditions = append(hr.Status.Parents[lastParentIdx].Conditions,
				metav1.Condition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					ObservedGeneration: 1,
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			)
		}
	}
}

func WithBackendRef(name, namespace string) Option {
	return func(obj client.Object) {
		hr, ok := obj.(*gateway.HTTPRoute)
		if ok {
			namespaceName := gateway.Namespace(namespace)
			if hr.Spec.Rules == nil {
				hr.Spec.Rules = []gateway.HTTPRouteRule{}
			}
			group := gateway.Group(gvk.InferencePool.Group)
			kind := gateway.Kind(gvk.InferencePool.Kind)
			hr.Spec.Rules = append(hr.Spec.Rules, gateway.HTTPRouteRule{
				BackendRefs: []gateway.HTTPBackendRef{
					{
						BackendRef: gateway.BackendRef{
							BackendObjectReference: gateway.BackendObjectReference{
								Name:      gateway.ObjectName(name),
								Namespace: &namespaceName,
								Kind:      &kind,
								Group:     &group,
							},
						},
					},
				},
			})
		}
	}
}

func WithParentStatus(gatewayName, namespace string, opt ...ParentOption) Option {
	return func(obj client.Object) {
		ip, ok := obj.(*inferencev1.InferencePool)
		if ok {
			if ip.Status.Parents == nil {
				ip.Status.Parents = []inferencev1.ParentStatus{}
			}
			poolStatus := inferencev1.ParentStatus{
				ParentRef: inferencev1.ParentReference{
					Name:      inferencev1.ObjectName(gatewayName),
					Namespace: inferencev1.Namespace(namespace),
				},
			}
			for _, opt := range opt {
				opt(&poolStatus)
			}
			ip.Status.Parents = append(ip.Status.Parents, poolStatus)
		}
	}
}

func AsDefaultStatus() ParentOption {
	return func(parentStatusRef *inferencev1.ParentStatus) {
		dName := "default"
		dKind := "Status"
		parentStatusRef.ParentRef.Name = inferencev1.ObjectName(dName)
		parentStatusRef.ParentRef.Kind = inferencev1.Kind(dKind)
		WithConditions(
			metav1.ConditionUnknown,
			string(inferencev1.InferencePoolConditionAccepted),
			infPoolPending,
			"Waiting for controller",
		)
	}
}

func WithConditions(status metav1.ConditionStatus, conType, reason, message string) ParentOption {
	return func(parentStatusRef *inferencev1.ParentStatus) {
		if parentStatusRef.Conditions == nil {
			parentStatusRef.Conditions = []metav1.Condition{}
		}
		parentStatusRef.Conditions = append(parentStatusRef.Conditions,
			metav1.Condition{
				Type:               conType,
				Status:             status,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: 1,
				LastTransitionTime: metav1.NewTime(time.Now()),
			},
		)
	}
}

func WithAcceptedConditions() ParentOption {
	return func(parentStatusRef *inferencev1.ParentStatus) {
		WithConditions(
			metav1.ConditionTrue,
			string(inferencev1.InferencePoolConditionAccepted),
			string(inferencev1.InferencePoolReasonAccepted),
			"Accepted by the parentRef Gateway",
		)(parentStatusRef)
		WithConditions(
			metav1.ConditionTrue,
			string(inferencev1.InferencePoolConditionResolvedRefs),
			string(inferencev1.InferencePoolReasonResolvedRefs),
			"Resolved ExtensionRef",
		)(parentStatusRef)
	}
}

func WithExtensionRef(kind, name string) Option {
	return func(obj client.Object) {
		ip, ok := obj.(*inferencev1.InferencePool)
		if ok {
			typedKind := inferencev1.Kind(kind)
			ip.Spec.EndpointPickerRef = inferencev1.EndpointPickerRef{
				Name: inferencev1.ObjectName(name),
				Kind: typedKind,
			}
		}
	}
}

// --- Object Creation Functions ---

func NewGateway(name string, opts ...Option) *gateway.Gateway {
	gw := &gateway.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultTestNS,
		},
		Spec: gateway.GatewaySpec{
			GatewayClassName: "istio",
		},
	}
	for _, opt := range opts {
		opt(gw)
	}
	return gw
}

func NewHTTPRoute(name string, opts ...Option) *gateway.HTTPRoute {
	hr := &gateway.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultTestNS,
		},
	}
	for _, opt := range opts {
		opt(hr)
	}
	return hr
}

func NewInferencePool(name string, opts ...Option) *inferencev1.InferencePool {
	ip := &inferencev1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultTestNS,
		},
		Spec: inferencev1.InferencePoolSpec{
			Selector: inferencev1.LabelSelector{
				MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{
					"app": "test",
				},
			},
			EndpointPickerRef: inferencev1.EndpointPickerRef{
				Name: "endpoint-picker",
			},
		},
	}
	for _, opt := range opts {
		opt(ip)
	}
	return ip
}

func NewService(name string, opts ...Option) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultTestNS,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(9002),
				},
			},
		},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}
