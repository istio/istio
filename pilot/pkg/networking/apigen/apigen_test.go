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

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
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

// Test using resolving DNS over GRPC. This uses XDS protocol, and Listener resources
// to represent the names. The protocol is based on GRPC resolution of XDS resources.
func TestAPIGenWithMulAddresses(t *testing.T) {
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
