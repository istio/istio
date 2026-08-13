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

package apigen_test

import (
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

// Creates an in-process discovery server, using the same code as Istiod, but
// backed by an in-memory config and endpoint Store.
func initDS(t *testing.T) *xds.FakeDiscoveryServer {
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	sd := ds.MemRegistry
	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)
	sd.SetEndpoints("fortio1.fortio.svc.cluster.local", "", []*model.IstioEndpoint{
		{
			Addresses:       []string{"127.0.0.1"},
			EndpointPort:    uint32(14056),
			ServicePortName: "http-main",
		},
	})
	return ds
}

// Creates an in-process discovery server with multiple addresses, using the same code as Istiod, but
// backed by an in-memory config and endpoint Store.
func initDSwithMulAddresses(t *testing.T) *xds.FakeDiscoveryServer {
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	sd := ds.MemRegistry
	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)
	sd.SetEndpoints("fortio1.fortio.svc.cluster.local", "", []*model.IstioEndpoint{
		{
			Addresses:       []string{"127.0.0.1", "2001:1::1"},
			EndpointPort:    uint32(14056),
			ServicePortName: "http-main",
		},
	})
	return ds
}

// Test using resolving DNS over GRPC. This uses XDS protocol, and Listener resources
// to represent the names. The protocol is based on GRPC resolution of XDS resources.
func TestAPIGen(t *testing.T) {
	// This test exercises the config-serving mechanics over an in-process connection that has no
	// verified identity; disable the control-plane-identity gate so it targets the generator itself.
	test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, false)
	ds := initDS(t)

	// Verify we can receive the DNS cluster IPs using XDS
	t.Run("adsc", func(t *testing.T) {
		proxy := &model.Proxy{Metadata: &model.NodeMetadata{
			Generator: "api",
		}}
		adscConn := ds.ConnectUnstarted(ds.SetupProxy(proxy), xds.APIWatches())
		store := memory.Make(collections.Pilot)
		configController := memory.NewController(store)
		adscConn.Store = configController
		err := adscConn.Run()
		if err != nil {
			t.Fatal("ADSC: failed running ", err)
		}

		_, err = adscConn.WaitVersion(10*time.Second, gvk.ServiceEntry.String(), "")
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}

		ses := adscConn.Store.List(gvk.ServiceEntry, "")
		for _, se := range ses {
			t.Log(se)
		}
		sec := adscConn.Store.List(gvk.EnvoyFilter, "")
		for _, se := range sec {
			t.Log(se)
		}
	})
}

// TestAPIGenRequiresControlPlaneIdentity verifies that the api generator only serves callers
// with a verified control-plane (root namespace) identity. Without this, an unauthenticated
// client on the plaintext xDS port (15010) or a workload in any namespace on the mTLS port can
// select GENERATOR=api and read cluster-wide Istio config across all namespaces.
func TestAPIGenRequiresControlPlaneIdentity(t *testing.T) {
	// The generator captures the feature flag at construction, so each subtest builds its
	// own generator after setting the flag.
	newGen := func() *apigen.APIGenerator {
		return apigen.NewGenerator(memory.NewController(memory.Make(collections.Pilot)))
	}
	w := &model.WatchedResource{TypeUrl: gvk.AuthorizationPolicy.String()}

	t.Run("unauthenticated rejected", func(t *testing.T) {
		test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, true)
		proxy := &model.Proxy{VerifiedIdentity: nil} // simulates plaintext port 15010
		_, _, err := newGen().Generate(proxy, w, nil)
		assert.Error(t, err)
	})

	t.Run("non-system namespace rejected", func(t *testing.T) {
		test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, true)
		proxy := &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "attacker"}}
		_, _, err := newGen().Generate(proxy, w, nil)
		assert.Error(t, err)
	})

	t.Run("control-plane identity allowed", func(t *testing.T) {
		test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, true)
		proxy := &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: constants.IstioSystemNamespace}}
		_, _, err := newGen().Generate(proxy, w, nil)
		assert.NoError(t, err)
	})

	t.Run("gate disabled allows unauthenticated", func(t *testing.T) {
		test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, false)
		proxy := &model.Proxy{VerifiedIdentity: nil}
		_, _, err := newGen().Generate(proxy, w, nil)
		assert.NoError(t, err)
	})
}

// TestAPIGenADSRejectsUnauthenticated exercises the control-plane-identity gate over a full
// ADS stream rather than calling authorize directly: a client that selects GENERATOR=api with
// no verified identity (as on the plaintext xDS port 15010) must be rejected. This covers the
// real path (generator selection in pushXds -> Generate -> authorize -> stream error) that the
// unit test above stubs out.
func TestAPIGenADSRejectsUnauthenticated(t *testing.T) {
	test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, true)
	ds := initDS(t)

	// The buffcon ADS connection has no verified identity, matching an unauthenticated caller.
	ads := ds.ConnectADS().
		WithType(gvk.ServiceEntry.String()).
		WithMetadata(model.NodeMetadata{Generator: "api"})
	ads.Request(t, &discovery.DiscoveryRequest{TypeUrl: gvk.ServiceEntry.String()})
	if err := ads.ExpectError(t); err == nil {
		t.Fatal("expected api generator to reject an ADS connection without a verified identity")
	}
}

// Test using resolving DNS over GRPC. This uses XDS protocol, and Listener resources
// to represent the names. The protocol is based on GRPC resolution of XDS resources.
func TestAPIGenWithMulAddresses(t *testing.T) {
	test.SetForTest(t, &features.EnableXDSAPIGeneratorAuth, false)
	ds := initDSwithMulAddresses(t)

	// Verify we can receive the DNS cluster IPs using XDS
	t.Run("adsc", func(t *testing.T) {
		proxy := &model.Proxy{Metadata: &model.NodeMetadata{
			Generator: "api",
		}}
		adscConn := ds.ConnectUnstarted(ds.SetupProxy(proxy), xds.APIWatches())
		store := memory.Make(collections.Pilot)
		configController := memory.NewController(store)
		adscConn.Store = configController
		err := adscConn.Run()
		if err != nil {
			t.Fatal("ADSC: failed running ", err)
		}

		_, err = adscConn.WaitVersion(10*time.Second, gvk.ServiceEntry.String(), "")
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}

		ses := adscConn.Store.List(gvk.ServiceEntry, "")
		for _, se := range ses {
			t.Log(se)
		}
		sec := adscConn.Store.List(gvk.EnvoyFilter, "")
		for _, se := range sec {
			t.Log(se)
		}
	})
}
