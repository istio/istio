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

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	destinationmodel "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

func TestCompileBackendBinding(t *testing.T) {
	http2 := gatewayx.BackendProtocolHTTP2
	backend := testXBackend("backend", "ns", "uid-1")
	backend.Spec.Protocol = &http2
	binding, err := compileBackendBinding(backend, types.NamespacedName{Namespace: "gw-ns", Name: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Endpoint.Hostname != "api.example.com" || binding.Port.Port != 8443 || binding.Port.Protocol != protocol.HTTP2 {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	if binding.Destination.Definition.Source.Kind != kind.XBackend || binding.Destination.Consumer.Kind != "Gateway" ||
		binding.Destination.Endpoints.Kind != destinationmodel.DNS || binding.Destination.Connection.Protocol != protocol.HTTP2 {
		t.Fatalf("unexpected source-neutral destination: %+v", binding.Destination)
	}
	if binding.Destination.RuntimeName != binding.InternalName || len(binding.Destination.Dependencies) != 1 {
		t.Fatalf("Gateway adapter diverged from destination contract: %+v", binding.Destination)
	}
	if strings.Contains(string(binding.InternalName), "api.example.com") {
		t.Fatalf("internal name exposes endpoint hostname: %s", binding.InternalName)
	}
	if got, want := binding.InternalName, backendInternalName("ns", "backend", "uid-1"); got != want {
		t.Fatalf("internal name is not deterministic: got %q want %q", got, want)
	}
	if got := backendInternalName("ns", "backend", "uid-2"); got == binding.InternalName {
		t.Fatal("delete/recreate UID did not change internal name")
	}
}

func TestBackendBindingsRequireAcceptedAttachment(t *testing.T) {
	stop := test.NewStop(t)
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	backend := testXBackend("backend", "ns", "uid-1")
	backendKey := TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "backend"}, Kind: kind.XBackend}
	routeKey := TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "route"}, Kind: kind.HTTPRoute}
	gw := types.NamespacedName{Namespace: "ns", Name: "gateway"}
	backends := krt.NewStaticCollection(nil, []*gatewayx.XBackend{backend}, opts.WithName("backends")...)
	ancestors := krt.NewStaticCollection(nil, []AncestorBackend{{Backend: backendKey, Source: routeKey, Gateway: gw}}, opts.WithName("ancestors")...)
	emptyAttachments := krt.NewStaticCollection[RouteAttachment](nil, nil, opts.WithName("empty-attachments")...)
	emptyGrants := gatewaycommon.BuildReferenceGrants(krt.NewStaticCollection[gatewaycommon.ReferenceGrant](nil, nil, opts.WithName("grants")...))

	withoutAttachment := BackendBindings(backends, ancestors, emptyAttachments, emptyGrants, opts)
	if !withoutAttachment.Active.WaitUntilSynced(stop) {
		t.Fatal("binding collection did not sync")
	}
	if got := withoutAttachment.Active.List(); len(got) != 0 {
		t.Fatalf("declared parent unexpectedly activated binding: %+v", got)
	}

	attachments := krt.NewStaticCollection(nil, []RouteAttachment{{
		From: TypedResource{Kind: gvk.HTTPRoute, Name: routeKey.NamespacedName}, To: gw,
	}}, opts.WithName("attachments")...)
	withAttachment := BackendBindings(backends, ancestors, attachments, emptyGrants, opts)
	if !withAttachment.Active.WaitUntilSynced(stop) {
		t.Fatal("binding collection did not sync")
	}
	if got := withAttachment.Active.List(); len(got) != 1 || got[0].Gateway != gw ||
		got[0].Destination.Port.Protocol != protocol.HTTP || got[0].Destination.Connection.Protocol != protocol.HTTP {
		t.Fatalf("accepted attachment did not activate binding: %+v", got)
	}
}

func TestInheritBackendProtocol(t *testing.T) {
	for _, tt := range []struct {
		kind kind.Kind
		want protocol.Instance
	}{
		{kind.HTTPRoute, protocol.HTTP},
		{kind.GRPCRoute, protocol.GRPC},
		{kind.TCPRoute, protocol.TCP},
		{kind.TLSRoute, protocol.TCP},
	} {
		binding, err := compileBackendBinding(testXBackend("backend", "ns", "uid-1"), types.NamespacedName{Namespace: "ns", Name: "gw"})
		if err != nil {
			t.Fatal(err)
		}
		if err := inheritBackendProtocol(binding, tt.kind); err != nil {
			t.Fatalf("%s: %v", tt.kind, err)
		}
		if binding.Destination.Port.Protocol != tt.want || binding.Destination.Connection.Protocol != tt.want ||
			binding.Port.Protocol != tt.want || binding.Destination.Key.Policy != "protocol:"+string(tt.want) ||
			binding.InternalName != backendInternalName("ns", "backend", "uid-1", string(tt.want)) ||
			binding.Destination.RuntimeName != binding.InternalName {
			t.Fatalf("%s inherited %+v, want %s", tt.kind, binding, tt.want)
		}
	}
}

func TestBuildDestinationXBackend(t *testing.T) {
	backend := testXBackend("backend", "ns", "uid-1")
	backends := krt.NewStaticCollection(nil, []*gatewayx.XBackend{backend})
	ctx := RouteContext{
		Krt:                krt.TestingDummyContext{},
		RouteContextInputs: RouteContextInputs{XBackends: backends},
	}
	group := gatewayv1.Group(gvk.XBackend.Group)
	kind := gatewayv1.Kind(gvk.XBackend.Kind)
	ref := gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
		Group: &group, Kind: &kind, Name: gatewayv1.ObjectName(backend.Name),
	}}
	destination, _, err := buildDestination(ctx, ref, backend.Namespace, true, gvk.HTTPRoute)
	if err != nil {
		t.Fatal(err)
	}
	if destination.Host != string(backendInternalName(backend.Namespace, backend.Name, backend.UID, string(protocol.HTTP))) ||
		destination.Port.GetNumber() != uint32(backend.Spec.Port.Port) {
		t.Fatalf("unexpected XBackend destination: %+v", destination)
	}

	wrongPort := gatewayv1.PortNumber(9443)
	ref.Port = &wrongPort
	if _, _, err := buildDestination(ctx, ref, backend.Namespace, true, gvk.HTTPRoute); err == nil {
		t.Fatal("route port differing from XBackend port was accepted")
	}
	if _, _, err := buildDestination(ctx, ref, backend.Namespace, false, gvk.HTTPRoute); err == nil {
		t.Fatal("mesh-parent XBackend was accepted")
	}
}

func testXBackend(name, namespace string, uid types.UID) *gatewayx.XBackend {
	return &gatewayx.XBackend{
		TypeMeta:   metav1.TypeMeta{APIVersion: gvk.XBackend.Group + "/" + gvk.XBackend.Version, Kind: gvk.XBackend.Kind},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid},
		Spec: gatewayx.BackendSpec{
			Type:             gatewayx.BackendTypeExternalHostname,
			Port:             gatewayx.BackendPort{Port: 8443},
			ExternalHostname: &gatewayx.ExternalHostnameBackend{Hostname: "api.example.com"},
		},
	}
}
