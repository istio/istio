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
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
)

func TestBackendPolicyExportTo(t *testing.T) {
	xbackend := TypedNamespacedName{
		NamespacedName: types.NamespacedName{Namespace: "consumer", Name: "backend"},
		Kind:           kind.XBackend,
	}
	policies := []BackendPolicy{
		{
			Gateways: []types.NamespacedName{
				{Namespace: "consumer", Name: "local"},
				{Namespace: "gateways-b", Name: "second"},
			},
		},
		{
			Gateways: []types.NamespacedName{
				{Namespace: "gateways-a", Name: "first"},
				{Namespace: "gateways-b", Name: "duplicate-namespace"},
			},
		},
	}

	assert.Equal(t, []string{".", "gateways-a", "gateways-b"}, backendPolicyExportTo(xbackend, policies))
	assert.Equal(t, []string{"."}, backendPolicyExportTo(xbackend, nil))
	assert.Nil(t, backendPolicyExportTo(TypedNamespacedName{Kind: kind.Service}, policies))
}

func TestClientCertificateCollections(t *testing.T) {
	opts := krttest.Options(t)
	gateway := types.NamespacedName{Namespace: "gateway-ns", Name: "gateway"}
	const certificate = "kubernetes-gateway://credentials/client-cert"
	policies := krt.NewStaticCollection(nil, []BackendPolicy{
		{
			Source:   TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: "app", Name: "one"}, Kind: kind.XBackend},
			Target:   TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: "app", Name: "one"}, Kind: kind.XBackend},
			Host:     "one.example.com",
			TLS:      &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_MUTUAL, CredentialName: certificate},
			Gateways: []types.NamespacedName{gateway},
		},
		{
			Source:   TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: "app", Name: "two"}, Kind: kind.XBackend},
			Target:   TypedNamespacedName{NamespacedName: types.NamespacedName{Namespace: "app", Name: "two"}, Kind: kind.XBackend},
			Host:     "two.example.com",
			TLS:      &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_MUTUAL, CredentialName: certificate},
			Gateways: []types.NamespacedName{gateway},
		},
		{
			Source: TypedNamespacedName{
				NamespacedName: types.NamespacedName{Namespace: "app", Name: "sidecar-only"},
				Kind:           kind.XBackend,
			},
			Target: TypedNamespacedName{
				NamespacedName: types.NamespacedName{Namespace: "app", Name: "sidecar-only"},
				Kind:           kind.XBackend,
			},
			Host: "sidecar.example.com",
			TLS:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_MUTUAL, CredentialName: certificate},
		},
	}, opts.WithName("Policies")...)

	destinationScopes := destinationRuleClientCertificateScopeCollection(policies, opts)
	gatewayScopes := gatewayClientCertificateScopeCollection(policies, opts)
	if !destinationScopes.WaitUntilSynced(opts.Stop()) || !gatewayScopes.WaitUntilSynced(opts.Stop()) {
		t.Fatal("client certificate collections did not sync")
	}
	items := destinationScopes.List()
	if len(items) != 3 {
		t.Fatalf("expected one destination policy per XBackend, got %d", len(items))
	}
	if items[0].ResourceName() == items[1].ResourceName() {
		t.Fatalf("policies from different XBackends have the same resource key %q", items[0].ResourceName())
	}

	byGatewayAndCertificate := krt.NewIndex(gatewayScopes, "byGatewayAndCertificate",
		func(r GatewayClientCertificateScope) []string {
			return []string{gatewayCertificateKey(r.Gateway, r.Certificate)}
		},
	)
	if got := len(byGatewayAndCertificate.Lookup(gatewayCertificateKey(gateway, certificate))); got != 2 {
		t.Fatalf("expected both XBackends to authorize the shared certificate, got %d", got)
	}
	byResourceName := krt.NewIndex(destinationScopes, "byResourceName", func(r DestinationRuleClientCertificateScope) []string {
		return []string{r.Certificate}
	})
	if got := len(byResourceName.Lookup(certificate)); got != 3 {
		t.Fatalf("expected sidecar-only XBackend to retain a destination policy, got %d", got)
	}
	if got := len(gatewayScopes.List()); got != 2 {
		t.Fatalf("expected sidecar-only XBackend not to create a Gateway authorization, got %d", got)
	}
}
