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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
)

func TestCompileXBackendConnectionPolicy(t *testing.T) {
	backend := testXBackend("backend", "ns", "uid-1")
	system := gatewayv1.WellKnownCACertificatesSystem
	backend.Spec.TLS = &gatewayx.BackendTLS{
		Mode: gatewayx.BackendTLSModeServerOnly,
		Validation: gatewayv1.BackendTLSPolicyValidation{
			Hostname:                "validation.example.com",
			WellKnownCACertificates: &system,
		},
	}
	binding, err := compileBackendBinding(backend, types.NamespacedName{Namespace: "gw-ns", Name: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	dr, err := compileXBackendConnectionPolicy(krt.TestingDummyContext{}, *binding,
		gatewaycommon.NewReferenceSet(), gatewaycommon.ReferenceGrants{})
	if err != nil {
		t.Fatal(err)
	}
	spec := dr.Spec.(*networking.DestinationRule)
	if spec.Host != string(binding.InternalName) {
		t.Fatalf("DestinationRule targets %q, want opaque host %q", spec.Host, binding.InternalName)
	}
	if spec.TrafficPolicy.Tls.Mode != networking.ClientTLSSettings_SIMPLE {
		t.Fatalf("unexpected TLS mode: %v", spec.TrafficPolicy.Tls.Mode)
	}
	if spec.TrafficPolicy.Tls.Sni != "validation.example.com" {
		t.Fatalf("SNI %q does not use the validation hostname", spec.TrafficPolicy.Tls.Sni)
	}
}

func TestXBackendRejectsMCP(t *testing.T) {
	backend := testXBackend("backend", "ns", "uid-1")
	mcp := gatewayx.BackendProtocolMCP
	backend.Spec.Protocol = &mcp
	_, err := compileBackendBinding(backend, types.NamespacedName{Namespace: "ns", Name: "gw"})
	if err == nil || !strings.Contains(err.Error(), "no MCP backend primitive") {
		t.Fatalf("expected explicit MCP rejection, got %v", err)
	}
}

func TestXBackendRejectsUnrepresentableMutualTLS(t *testing.T) {
	backend := testXBackend("backend", "ns", "uid-1")
	system := gatewayv1.WellKnownCACertificatesSystem
	backend.Spec.TLS = &gatewayx.BackendTLS{
		Mode: gatewayx.BackendTLSModeClientAndServer,
		ClientCertificateRef: &gatewayv1.SecretObjectReference{
			Name: "client-identity",
		},
		Validation: gatewayv1.BackendTLSPolicyValidation{
			Hostname:                "api.example.com",
			WellKnownCACertificates: &system,
		},
	}
	binding, err := compileBackendBinding(backend, types.NamespacedName{Namespace: "ns", Name: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compileXBackendConnectionPolicy(krt.TestingDummyContext{}, *binding,
		gatewaycommon.NewReferenceSet(), gatewaycommon.ReferenceGrants{})
	if err == nil || !strings.Contains(err.Error(), "separate client and CA credentials") {
		t.Fatalf("expected explicit mutual TLS rejection, got %v", err)
	}
}

func TestXBackendStatusPreservesOtherControllersAndPrunesStaleIstio(t *testing.T) {
	backend := testXBackend("backend", "backend-ns", "uid-1")
	backend.Generation = 3
	other := gatewayx.BackendAncestorStatus{
		ControllerName: "other.example/controller",
		AncestorRef:    gatewayv1.ParentReference{Name: "other"},
	}
	stale := gatewayx.BackendAncestorStatus{
		ControllerName: gatewayv1.GatewayController(features.ManagedGatewayController),
		AncestorRef:    gatewayv1.ParentReference{Name: "stale"},
	}
	backend.Status.Ancestors = []gatewayx.BackendAncestorStatus{other, stale}
	source := TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}, Kind: kind.XBackend}
	gateway := types.NamespacedName{Namespace: "gateway-ns", Name: "gateway"}
	binding, err := compileBackendBinding(backend, gateway)
	if err != nil {
		t.Fatal(err)
	}
	status := xBackendStatus(backend,
		[]BackendBindingResult{{Source: source, Gateway: gateway, Binding: binding}},
		[]XBackendConnectionPolicyResult{{Binding: *binding}},
	)
	if len(status.Ancestors) != 2 || status.Ancestors[0].ControllerName != other.ControllerName {
		t.Fatalf("other-controller entry was not preserved: %+v", status.Ancestors)
	}
	got := status.Ancestors[1]
	if got.AncestorRef.Namespace == nil || *got.AncestorRef.Namespace != "gateway-ns" {
		t.Fatalf("unexpected Gateway ancestor ref: %+v", got.AncestorRef)
	}
	if len(got.Conditions) != 1 || got.Conditions[0].Status != metav1.ConditionTrue || got.Conditions[0].ObservedGeneration != 3 {
		t.Fatalf("unexpected accepted condition: %+v", got.Conditions)
	}
}

func TestXBackendStatusReportsPolicyFailure(t *testing.T) {
	backend := testXBackend("backend", "ns", "uid-1")
	gateway := types.NamespacedName{Namespace: "ns", Name: "gateway"}
	binding, err := compileBackendBinding(backend, gateway)
	if err != nil {
		t.Fatal(err)
	}
	status := xBackendStatus(backend,
		[]BackendBindingResult{{Source: binding.Source, Gateway: gateway, Binding: binding}},
		[]XBackendConnectionPolicyResult{{Binding: *binding, Error: "bad TLS reference"}},
	)
	condition := status.Ancestors[0].Conditions[0]
	if condition.Status != metav1.ConditionFalse || condition.Reason != "Invalid" || !strings.Contains(condition.Message, "bad TLS") {
		t.Fatalf("unexpected invalid condition: %+v", condition)
	}
}
