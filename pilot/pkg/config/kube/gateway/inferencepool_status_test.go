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
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

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
)

func TestInferencePoolStatusReconciliation(t *testing.T) {
	testCases := []struct {
		name         string
		givens       []runtime.Object                 // Objects to create before the test
		targetPool   *inferencev1alpha2.InferencePool // The InferencePool to check
		expectations func(t *testing.T, pool *inferencev1alpha2.InferencePoolStatus)
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
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS)),
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", status.Parents[0].GatewayRef.Name)
				assert.Equal(t, DefaultTestNS, status.Parents[0].GatewayRef.Namespace)
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1alpha2.InferencePoolConditionAccepted),
					Status:  metav1.ConditionTrue,
					Reason:  string(inferencev1alpha2.InferencePoolReasonAccepted),
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "gateway-1", status.Parents[0].GatewayRef.Name)
				assert.Equal(t, DefaultTestNS, status.Parents[0].GatewayRef.Namespace)
			},
		},
		{
			name: "should keep the status of the gateway parentRefs from antoher controller",
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 2, "Expected two parent references")
				assert.ElementsMatch(t,
					[]string{"gateway-1", "gateway-2"},
					[]string{status.Parents[0].GatewayRef.Name, status.Parents[1].GatewayRef.Name},
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 2, "Expected two parent references")
				assert.ElementsMatch(t,
					[]string{"gateway-1", "gateway-2"},
					[]string{status.Parents[0].GatewayRef.Name, status.Parents[1].GatewayRef.Name},
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
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithParentStatus("gateway-2", DefaultTestNS, WithAcceptedConditions())),
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "gateway-1", status.Parents[0].GatewayRef.Name)
			},
		},
		{
			name: "should keep others status from previous reconciliation that is no longer referenced by any HTTPRoute",
			givens: []runtime.Object{
				NewGateway("gateway-1", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
				NewGateway("gateway-2", InNamespace(DefaultTestNS), WithGatewayClass("other-class")),
				NewHTTPRoute("route-1", InNamespace(DefaultTestNS),
					WithParentRefAndStatus("gateway-1", DefaultTestNS, IstioController),
					WithBackendRef("test-pool", DefaultTestNS)),
			},
			targetPool: NewInferencePool("test-pool", InNamespace(DefaultTestNS), WithParentStatus("gateway-2", DefaultTestNS, WithAcceptedConditions())),
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 2, "Expected two parent references")
				assert.ElementsMatch(t,
					[]string{"gateway-1", "gateway-2"},
					[]string{status.Parents[0].GatewayRef.Name, status.Parents[1].GatewayRef.Name},
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", status.Parents[0].GatewayRef.Name)
				assert.Equal(t, GatewayTestNS, status.Parents[0].GatewayRef.Namespace)
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", status.Parents[0].GatewayRef.Name)
				assert.Equal(t, GatewayTestNS, status.Parents[0].GatewayRef.Namespace)
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", status.Parents[0].GatewayRef.Name)
				assert.Equal(t, GatewayTestNS, status.Parents[0].GatewayRef.Namespace)
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				assert.Equal(t, "main-gateway", status.Parents[0].GatewayRef.Name)
				assert.Equal(t, AppTestNS, status.Parents[0].GatewayRef.Namespace)
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected only one parent reference for the same gateway")
				assert.Equal(t, "main-gateway", status.Parents[0].GatewayRef.Name)
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two condition")
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1alpha2.ModelConditionResolvedRefs),
					Status:  metav1.ConditionTrue,
					Reason:  string(inferencev1alpha2.ModelReasonResolvedRefs),
					Message: "Referenced ExtensionRef resolved",
				}, "Expected condition with InvalidExtensionRef")
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two condition")
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1alpha2.ModelConditionResolvedRefs),
					Status:  metav1.ConditionFalse,
					Reason:  string(inferencev1alpha2.ModelReasonInvalidExtensionRef),
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
			expectations: func(t *testing.T, status *inferencev1alpha2.InferencePoolStatus) {
				require.Len(t, status.Parents, 1, "Expected one parent reference")
				require.Len(t, status.Parents[0].Conditions, 2, "Expected two condition")
				assertConditionContains(t, status.Parents[0].Conditions, metav1.Condition{
					Type:    string(inferencev1alpha2.ModelConditionResolvedRefs),
					Status:  metav1.ConditionFalse,
					Reason:  string(inferencev1alpha2.ModelReasonInvalidExtensionRef),
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

			getInferencePoolStatus := func() *inferencev1alpha2.InferencePoolStatus {
				statuses := sq.Statuses()
				for _, status := range statuses {
					if pool, ok := status.(*inferencev1alpha2.InferencePoolStatus); ok {
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

type ParentOption func(*inferencev1alpha2.PoolStatus)

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
		ip, ok := obj.(*inferencev1alpha2.InferencePool)
		if ok {
			if ip.Status.Parents == nil {
				ip.Status.Parents = []inferencev1alpha2.PoolStatus{}
			}
			poolStatus := inferencev1alpha2.PoolStatus{
				GatewayRef: corev1.ObjectReference{
					Name:      gatewayName,
					Namespace: namespace,
				},
			}
			for _, opt := range opt {
				opt(&poolStatus)
			}
			ip.Status.Parents = append(ip.Status.Parents, poolStatus)
		}
	}
}

func WithAcceptedConditions() ParentOption {
	return func(parentStatusRef *inferencev1alpha2.PoolStatus) {
		// Add condition to the first parent status
		if parentStatusRef.Conditions == nil {
			parentStatusRef.Conditions = []metav1.Condition{}
		}
		parentStatusRef.Conditions = append(parentStatusRef.Conditions,
			metav1.Condition{
				Type:               string(inferencev1alpha2.InferencePoolConditionAccepted),
				Status:             metav1.ConditionTrue,
				Reason:             string(inferencev1alpha2.InferencePoolReasonAccepted),
				Message:            "Accepted by the parentRef Gateway",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.NewTime(time.Now()),
			},
			metav1.Condition{
				Type:               string(inferencev1alpha2.ModelConditionResolvedRefs),
				Status:             metav1.ConditionTrue,
				Reason:             string(inferencev1alpha2.ModelReasonResolvedRefs),
				Message:            "Resolved Ex",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.NewTime(time.Now()),
			})
	}
}

func WithExtensionRef(kind, name string) Option {
	return func(obj client.Object) {
		ip, ok := obj.(*inferencev1alpha2.InferencePool)
		if ok {
			typedKind := inferencev1alpha2.Kind(kind)
			ip.Spec.EndpointPickerConfig.ExtensionRef = &inferencev1alpha2.Extension{
				ExtensionReference: inferencev1alpha2.ExtensionReference{
					Name: inferencev1alpha2.ObjectName(name),
					Kind: &typedKind,
				},
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

func NewInferencePool(name string, opts ...Option) *inferencev1alpha2.InferencePool {
	ip := &inferencev1alpha2.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultTestNS,
		},
		Spec: inferencev1alpha2.InferencePoolSpec{
			Selector: map[inferencev1alpha2.LabelKey]inferencev1alpha2.LabelValue{
				"app": "test",
			},
			EndpointPickerConfig: inferencev1alpha2.EndpointPickerConfig{
				ExtensionRef: &inferencev1alpha2.Extension{
					ExtensionReference: inferencev1alpha2.ExtensionReference{
						Name: "endpoint-picker",
					},
				},
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
