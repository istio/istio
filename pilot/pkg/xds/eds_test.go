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
package xds_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	uatomic "go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

// The connect and reconnect tests are removed - ADS already has coverage, and the
// StreamEndpoints is not used in 1.0+

const (
	asdcLocality  = "region1/zone1/subzone1"
	asdc2Locality = "region2/zone2/subzone2"

	edsIncSvc = "eds.test.svc.cluster.local"
	edsIncVip = "10.10.1.2"
)

func TestIncrementalPush(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-all.yaml") +
			mustReadFile(t, "tests/testdata/config/static-weighted-se.yaml"),
	})
	ads := s.Connect(nil, nil, watchAll)
	t.Run("Full Push", func(t *testing.T) {
		s.Discovery.Push(&model.PushRequest{Full: true})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Incremental Push", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{Full: false})
		if err := ads.WaitSingle(time.Second*5, v3.EndpointType, v3.ClusterType); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Incremental Push with updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			Full: false,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "destall.default.svc.cluster.local", Namespace: "testns", Kind: gvk.ServiceEntry}: {},
			},
		})
		if err := ads.WaitSingle(time.Second*5, v3.EndpointType, v3.ClusterType); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Full Push with updated services", func(t *testing.T) {
		ads.WaitClear()

		s.Discovery.Push(&model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "weighted.static.svc.cluster.local", Namespace: "default", Kind: gvk.ServiceEntry}: {},
			},
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 1 {
			t.Fatalf("Expected a partial EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push with multiple updates", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "foo.bar", Namespace: "default", Kind: gvk.ServiceEntry}:   {},
				{Name: "destall", Namespace: "testns", Kind: gvk.DestinationRule}: {},
			},
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 4 {
			t.Fatalf("Expected a full EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push without updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "destall", Namespace: "testns", Kind: gvk.DestinationRule}: {},
			},
		})
		if _, err := ads.Wait(time.Second*5, v3.ClusterType, v3.EndpointType); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) < 3 {
			t.Fatalf("Expected a full EDS update, but got: %v", ads.GetEndpoints())
		}
	})
}

func TestEds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-locality.yaml"),
		DiscoveryServerModifier: func(s *xds.DiscoveryServer) {
			addUdsEndpoint(s)

			// enable locality load balancing and add relevant endpoints in order to test
			addLocalityEndpoints(s, "locality.cluster.local")
			addLocalityEndpoints(s, "locality-no-outlier-detection.cluster.local")

			// Add the test ads clients to list of service instances in order to test the context dependent locality coloring.
			addTestClientEndpoints(s)

			s.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
			s.MemRegistry.SetEndpoints(edsIncSvc, "",
				newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))
		},
	})

	adscConn := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.10"}}, nil, watchAll)
	adscConn2 := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.11"}}, nil, watchAll)

	t.Run("TCPEndpoints", func(t *testing.T) {
		testTCPEndpoints("127.0.0.1", adscConn, t)
	})
	t.Run("edsz", func(t *testing.T) {
		testEdsz(t, s, "test-1.default")
	})
	t.Run("LocalityPrioritizedEndpoints", func(t *testing.T) {
		testLocalityPrioritizedEndpoints(adscConn, adscConn2, t)
	})
	t.Run("UDSEndpoints", func(t *testing.T) {
		testUdsEndpoints(adscConn, t)
	})
	t.Run("PushIncremental", func(t *testing.T) {
		edsUpdateInc(s, adscConn, t)
	})
	t.Run("Push", func(t *testing.T) {
		edsUpdates(s, adscConn, t)
	})
	t.Run("MultipleRequest", func(t *testing.T) {
		multipleRequest(s, false, 20, 5, 25*time.Second, nil, t)
	})
	// 5 pushes for 100 clients, using EDS incremental only.
	t.Run("MultipleRequestIncremental", func(t *testing.T) {
		multipleRequest(s, true, 20, 5, 25*time.Second, nil, t)
	})
	t.Run("CDSSave", func(t *testing.T) {
		// Moved from cds_test, using new client
		clusters := adscConn.GetClusters()
		if len(clusters) == 0 {
			t.Error("No clusters in ADS response")
		}
		strResponse, _ := json.MarshalIndent(clusters, " ", " ")
		_ = os.WriteFile(env.IstioOut+"/cdsv2_sidecar.json", strResponse, 0o644)
	})
}

// newEndpointWithAccount is a helper for IstioEndpoint creation. Creates endpoints with
// port name "http", with the given IP, service account and a 'version' label.
// nolint: unparam
func newEndpointWithAccount(ip, account, version string) []*model.IstioEndpoint {
	return []*model.IstioEndpoint{
		{
			Address:         ip,
			ServicePortName: "http-main",
			EndpointPort:    80,
			Labels:          map[string]string{"version": version},
			ServiceAccount:  account,
		},
	}
}

func TestTunnelServerEndpointEds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	s.Discovery.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		[]*model.IstioEndpoint{
			{
				Address:         "127.0.0.1",
				ServicePortName: "http-main",
				EndpointPort:    80,
				// Labels:          map[string]string{"version": version},
				ServiceAccount: "hello-sa",
				TunnelAbility:  networking.MakeTunnelAbility(networking.H2Tunnel),
			},
		})

	t.Run("TestClientWantsTunnelEndpoints", func(t *testing.T) {
		t.Helper()
		adscConn1 := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.10"}, Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{
				ProxyMetadata: map[string]string{
					"tunnel": networking.H2TunnelTypeName,
				},
			},
		}}, nil, watchAll)
		testTunnelEndpoints("127.0.0.1", 15009, adscConn1, t)
	})
	t.Run("TestClientWantsNoTunnelEndpoints", func(t *testing.T) {
		t.Helper()
		adscConn2 := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.11"}, Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{},
		}}, nil, watchAll)
		testTunnelEndpoints("127.0.0.1", 80, adscConn2, t)
	})
}

func TestNoTunnelServerEndpointEds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

	// Add the test ads clients to list of service instances in order to test the context dependent locality coloring.
	addTestClientEndpoints(s.Discovery)

	s.Discovery.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		[]*model.IstioEndpoint{
			{
				Address:         "127.0.0.1",
				ServicePortName: "http-main",
				EndpointPort:    80,
				// Labels:          map[string]string{"version": version},
				ServiceAccount: "hello-sa",
				// No Tunnel Support at this endpoint.
				TunnelAbility: networking.MakeTunnelAbility(),
			},
		})

	t.Run("TestClientWantsTunnelEndpoints", func(t *testing.T) {
		adscConn := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.10"}, Metadata: &model.NodeMetadata{
			ProxyConfig: &model.NodeMetaProxyConfig{
				ProxyMetadata: map[string]string{
					"tunnel": networking.H2TunnelTypeName,
				},
			},
		}}, nil, watchAll)
		testTunnelEndpoints("127.0.0.1", 80, adscConn, t)
	})

	t.Run("TestClientWantsNoTunnelEndpoints", func(t *testing.T) {
		adscConn := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.11"}, Metadata: &model.NodeMetadata{}}, nil, watchAll)
		testTunnelEndpoints("127.0.0.1", 80, adscConn, t)
	})
}

func mustReadFile(t *testing.T, fpaths ...string) string {
	result := ""
	for _, fpath := range fpaths {
		if !strings.HasPrefix(fpath, ".") {
			fpath = filepath.Join(env.IstioSrc, fpath)
		}
		bytes, err := os.ReadFile(fpath)
		if err != nil {
			t.Fatal(err)
		}
		result += "---\n"
		result += string(bytes)
	}
	return result
}

func mustReadfolder(t *testing.T, folder string) string {
	result := ""
	fpathRoot := folder
	if !strings.HasPrefix(fpathRoot, ".") {
		fpathRoot = filepath.Join(env.IstioSrc, folder)
	}
	f, err := os.ReadDir(fpathRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, fpath := range f {
		bytes, err := os.ReadFile(filepath.Join(fpathRoot, fpath.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result += "---\n"
		result += string(bytes)
	}
	return result
}

func TestEdsWeightedServiceEntry(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: mustReadFile(t, "tests/testdata/config/static-weighted-se.yaml")})
	adscConn := s.Connect(nil, nil, watchEds)
	endpoints := adscConn.GetEndpoints()
	lbe, f := endpoints["outbound|80||weighted.static.svc.cluster.local"]
	if !f || len(lbe.Endpoints) == 0 {
		t.Fatalf("No lb endpoints for %v, %v", "outbound|80||weighted.static.svc.cluster.local", adscConn.EndpointsJSON())
	}
	expected := map[string]uint32{
		"a":       9, // sum of 1 and 8
		"b":       3,
		"3.3.3.3": 1, // no weight provided is normalized to 1
		"2.2.2.2": 8,
		"1.1.1.1": 3,
	}
	got := make(map[string]uint32)
	for _, lbe := range lbe.Endpoints {
		got[lbe.Locality.Region] = lbe.LoadBalancingWeight.Value
		for _, e := range lbe.LbEndpoints {
			got[e.GetEndpoint().Address.GetSocketAddress().Address] = e.LoadBalancingWeight.Value
		}
	}
	if !reflect.DeepEqual(expected, got) {
		t.Errorf("Expected LB weights %v got %v", expected, got)
	}
}

var (
	watchEds = []string{v3.ClusterType, v3.EndpointType}
	watchAll = []string{v3.ClusterType, v3.EndpointType, v3.ListenerType, v3.RouteType}
)

func TestEDSOverlapping(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addOverlappingEndpoints(s)
	adscon := s.Connect(nil, nil, watchEds)
	testOverlappingPorts(s, adscon, t)
}

func TestEDSUnhealthyEndpoints(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addUnhealthyCluster(s)
	adscon := s.Connect(nil, nil, watchEds)
	_, err := adscon.Wait(5 * time.Second)
	if err != nil {
		t.Fatalf("Error in push %v", err)
	}

	validateEndpoints := func(expectPush bool, healthy []string, unhealthy []string) {
		t.Helper()
		// Normalize lists to make comparison easier
		if healthy == nil {
			healthy = []string{}
		}
		if unhealthy == nil {
			unhealthy = []string{}
		}
		sort.Strings(healthy)
		sort.Strings(unhealthy)
		if expectPush {
			upd, _ := adscon.Wait(5*time.Second, v3.EndpointType)

			if len(upd) > 0 && !contains(upd, v3.EndpointType) {
				t.Fatalf("Expecting EDS push as endpoint health is changed. But received %v", upd)
			}
		} else {
			upd, _ := adscon.Wait(50*time.Millisecond, v3.EndpointType)
			if contains(upd, v3.EndpointType) {
				t.Fatalf("Expected no EDS push, got %v", upd)
			}
		}

		// Validate that endpoints are pushed.
		lbe := adscon.GetEndpoints()["outbound|53||unhealthy.svc.cluster.local"]
		eh, euh := xdstest.ExtractHealthEndpoints(lbe)
		gotHealthy := sets.NewWith(eh...).SortedList()
		gotUnhealthy := sets.NewWith(euh...).SortedList()
		if !reflect.DeepEqual(gotHealthy, healthy) {
			t.Fatalf("did not get expected endpoints: got %v, want %v", gotHealthy, healthy)
		}
		if !reflect.DeepEqual(gotUnhealthy, unhealthy) {
			t.Fatalf("did not get expected unhealthy endpoints: got %v, want %v", gotUnhealthy, unhealthy)
		}
	}

	// Validate that we do not send initial unhealthy endpoints.
	validateEndpoints(false, nil, nil)
	adscon.WaitClear()

	// Set additional unhealthy endpoint and validate Eds update is not triggered.
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.0.0.53",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.UnHealthy,
			},
			{
				Address:         "10.0.0.54",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.UnHealthy,
			},
		})

	// Validate that endpoint is not pushed.
	validateEndpoints(false, nil, nil)

	// Change the status of endpoint to Healthy and validate Eds is pushed.
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.0.0.53",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
			{
				Address:         "10.0.0.54",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
		})

	// Validate that endpoints are pushed.
	validateEndpoints(true, []string{"10.0.0.53:53", "10.0.0.54:53"}, nil)

	// Set to exact same endpoints
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.0.0.53",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
			{
				Address:         "10.0.0.54",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
		})
	// Validate that endpoint is not pushed.
	validateEndpoints(false, []string{"10.0.0.53:53", "10.0.0.54:53"}, nil)

	// Now change the status of endpoint to UnHealthy and validate Eds is pushed.
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.0.0.53",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.UnHealthy,
			},
			{
				Address:         "10.0.0.54",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
		})

	// Validate that endpoints are pushed.
	validateEndpoints(true, []string{"10.0.0.54:53"}, []string{"10.0.0.53:53"})

	// Change the status of endpoint to Healthy and validate Eds is pushed.
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.0.0.53",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
			{
				Address:         "10.0.0.54",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
		})

	validateEndpoints(true, []string{"10.0.0.54:53", "10.0.0.53:53"}, nil)

	// Remove a healthy endpoint
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.0.0.53",
				EndpointPort:    53,
				ServicePortName: "tcp-dns",
				HealthStatus:    model.Healthy,
			},
		})

	validateEndpoints(true, []string{"10.0.0.53:53"}, nil)

	// Remove last healthy endpoint
	s.Discovery.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "", []*model.IstioEndpoint{})
	validateEndpoints(true, nil, nil)
}

// Validates the behavior when Service resolution type is updated after initial EDS push.
// See https://github.com/istio/istio/issues/18355 for more details.
func TestEDSServiceResolutionUpdate(t *testing.T) {
	for _, resolution := range []model.Resolution{model.DNSLB, model.DNSRoundRobinLB} {
		t.Run(fmt.Sprintf("resolution_%s", resolution), func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
			addEdsCluster(s, "edsdns.svc.cluster.local", "http", "10.0.0.53", 8080)
			addEdsCluster(s, "other.local", "http", "1.1.1.1", 8080)

			adscConn := s.Connect(nil, nil, watchAll)

			// Validate that endpoints are pushed correctly.
			testEndpoints("10.0.0.53", "outbound|8080||edsdns.svc.cluster.local", adscConn, t)

			// Now update the service resolution to DNSLB/DNSRRLB with a DNS endpoint.
			updateServiceResolution(s, resolution)

			if _, err := adscConn.Wait(5*time.Second, v3.EndpointType); err != nil {
				t.Fatal(err)
			}

			// Validate that endpoints are skipped.
			lbe := adscConn.GetEndpoints()["outbound|8080||edsdns.svc.cluster.local"]
			if lbe != nil && len(lbe.Endpoints) > 0 {
				t.Fatalf("endpoints not expected for  %s,  but got %v", "edsdns.svc.cluster.local", adscConn.EndpointsJSON())
			}
		})
	}
}

// Validate that when endpoints of a service flipflop between 1 and 0 does not trigger a full push.
func TestEndpointFlipFlops(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addEdsCluster(s, "flipflop.com", "http", "10.0.0.53", 8080)

	adscConn := s.Connect(nil, nil, watchAll)

	// Validate that endpoints are pushed correctly.
	testEndpoints("10.0.0.53", "outbound|8080||flipflop.com", adscConn, t)

	// Clear the endpoint and validate it does not trigger a full push.
	s.Discovery.MemRegistry.SetEndpoints("flipflop.com", "", []*model.IstioEndpoint{})

	upd, _ := adscConn.Wait(5*time.Second, v3.EndpointType)

	if contains(upd, "cds") {
		t.Fatalf("Expecting only EDS update as part of a partial push. But received CDS also %v", upd)
	}

	if len(upd) > 0 && !contains(upd, v3.EndpointType) {
		t.Fatalf("Expecting EDS push as part of a partial push. But received %v", upd)
	}

	lbe := adscConn.GetEndpoints()["outbound|8080||flipflop.com"]
	if len(lbe.Endpoints) != 0 {
		t.Fatalf("There should be no endpoints for outbound|8080||flipflop.com. Endpoints:\n%v", adscConn.EndpointsJSON())
	}

	// Validate that keys in service still exist in EndpointShardsByService - this prevents full push.
	if len(s.Discovery.EndpointShardsByService["flipflop.com"]) == 0 {
		t.Fatalf("Expected service key %s to be present in EndpointShardsByService. But missing %v", "flipflop.com", s.Discovery.EndpointShardsByService)
	}

	// Set the endpoints again and validate it does not trigger full push.
	s.Discovery.MemRegistry.SetEndpoints("flipflop.com", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.10.1.1",
				ServicePortName: "http",
				EndpointPort:    8080,
			},
		})

	upd, _ = adscConn.Wait(5*time.Second, v3.EndpointType)

	if contains(upd, v3.ClusterType) {
		t.Fatalf("expecting only EDS update as part of a partial push. But received CDS also %+v", upd)
	}

	if len(upd) > 0 && !contains(upd, v3.EndpointType) {
		t.Fatalf("expecting EDS push as part of a partial push. But did not receive %+v", upd)
	}

	testEndpoints("10.10.1.1", "outbound|8080||flipflop.com", adscConn, t)
}

// Validate that deleting a service clears entries from EndpointShardsByService.
func TestDeleteService(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addEdsCluster(s, "removeservice.com", "http", "10.0.0.53", 8080)

	adscConn := s.Connect(nil, nil, watchEds)

	// Validate that endpoints are pushed correctly.
	testEndpoints("10.0.0.53", "outbound|8080||removeservice.com", adscConn, t)

	s.Discovery.MemRegistry.RemoveService("removeservice.com")

	if len(s.Discovery.EndpointShardsByService["removeservice.com"]) != 0 {
		t.Fatalf("Expected service key %s to be deleted in EndpointShardsByService. But is still there %v",
			"removeservice.com", s.Discovery.EndpointShardsByService)
	}
}

func TestUpdateServiceAccount(t *testing.T) {
	cluster1Endppoints := []*model.IstioEndpoint{
		{Address: "10.172.0.1", ServiceAccount: "sa1"},
		{Address: "10.172.0.2", ServiceAccount: "sa-vm1"},
	}

	testCases := []struct {
		name      string
		shardKey  model.ShardKey
		endpoints []*model.IstioEndpoint
		expect    bool
	}{
		{
			name:      "added new endpoint",
			shardKey:  "c1",
			endpoints: append(cluster1Endppoints, &model.IstioEndpoint{Address: "10.172.0.3", ServiceAccount: "sa1"}),
			expect:    false,
		},
		{
			name:      "added new sa",
			shardKey:  "c1",
			endpoints: append(cluster1Endppoints, &model.IstioEndpoint{Address: "10.172.0.3", ServiceAccount: "sa2"}),
			expect:    true,
		},
		{
			name:     "updated endpoints address",
			shardKey: "c1",
			endpoints: []*model.IstioEndpoint{
				{Address: "10.172.0.5", ServiceAccount: "sa1"},
				{Address: "10.172.0.2", ServiceAccount: "sa-vm1"},
			},
			expect: false,
		},
		{
			name:     "deleted one endpoint with unique sa",
			shardKey: "c1",
			endpoints: []*model.IstioEndpoint{
				{Address: "10.172.0.1", ServiceAccount: "sa1"},
			},
			expect: true,
		},
		{
			name:     "deleted one endpoint with duplicate sa",
			shardKey: "c1",
			endpoints: []*model.IstioEndpoint{
				{Address: "10.172.0.2", ServiceAccount: "sa-vm1"},
			},
			expect: false,
		},
		{
			name:      "deleted endpoints",
			shardKey:  "c1",
			endpoints: nil,
			expect:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := new(xds.DiscoveryServer)
			originalEndpointsShard := &xds.EndpointShards{
				Shards: map[model.ShardKey][]*model.IstioEndpoint{
					"c1": cluster1Endppoints,
					"c2": {{Address: "10.244.0.1", ServiceAccount: "sa1"}, {Address: "10.244.0.2", ServiceAccount: "sa-vm2"}},
				},
				ServiceAccounts: map[string]struct{}{
					"sa1":    {},
					"sa-vm1": {},
					"sa-vm2": {},
				},
			}
			originalEndpointsShard.Shards[tc.shardKey] = tc.endpoints
			ret := s.UpdateServiceAccount(originalEndpointsShard, "test-svc")
			if ret != tc.expect {
				t.Errorf("expect UpdateServiceAccount %v, but got %v", tc.expect, ret)
			}
		})
	}
}

func TestZeroEndpointShardSA(t *testing.T) {
	cluster1Endppoints := []*model.IstioEndpoint{
		{Address: "10.172.0.1", ServiceAccount: "sa1"},
	}
	s := new(xds.DiscoveryServer)
	originalEndpointsShard := &xds.EndpointShards{
		Shards: map[model.ShardKey][]*model.IstioEndpoint{
			"c1": cluster1Endppoints,
		},
		ServiceAccounts: map[string]struct{}{
			"sa1": {},
		},
	}
	s.EndpointShardsByService = make(map[string]map[string]*xds.EndpointShards)
	s.EndpointShardsByService["test"] = make(map[string]*xds.EndpointShards)
	s.EndpointShardsByService["test"]["test"] = originalEndpointsShard
	s.Cache = model.DisabledCache{}
	s.EDSCacheUpdate("c1", "test", "test", []*model.IstioEndpoint{})
	if len(s.EndpointShardsByService["test"]["test"].ServiceAccounts) != 0 {
		t.Errorf("endpoint shard service accounts got %v want 0", len(s.EndpointShardsByService["test"]["test"].ServiceAccounts))
	}
}

func fullPush(s *xds.FakeDiscoveryServer) {
	s.Discovery.Push(&model.PushRequest{Full: true})
}

func addTestClientEndpoints(server *xds.DiscoveryServer) {
	server.MemRegistry.AddService(&model.Service{
		Hostname: "test-1.default",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		},
	})
	server.MemRegistry.AddInstance("test-1.default", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.10.10.10",
			ServicePortName: "http",
			EndpointPort:    80,
			Locality:        model.Locality{Label: asdcLocality},
		},
		ServicePort: &model.Port{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		},
	})
	server.MemRegistry.AddInstance("test-1.default", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.10.10.11",
			ServicePortName: "http",
			EndpointPort:    80,
			Locality:        model.Locality{Label: asdc2Locality},
		},
		ServicePort: &model.Port{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		},
	})
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testTCPEndpoints(expected string, adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	testEndpoints(expected, "outbound|8080||eds.test.svc.cluster.local", adsc, t)
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testEndpoints(expected string, cluster string, adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	lbe, f := adsc.GetEndpoints()[cluster]
	if !f || len(lbe.Endpoints) == 0 {
		t.Fatalf("No lb endpoints for %v, %v", cluster, adsc.EndpointsJSON())
	}
	var found []string
	for _, lbe := range lbe.Endpoints {
		for _, e := range lbe.LbEndpoints {
			addr := e.GetEndpoint().Address.GetSocketAddress().Address
			found = append(found, addr)
			if expected == addr {
				return
			}
		}
	}
	t.Fatalf("Expecting %s got %v", expected, found)
}

// Verify server sends the tunneled endpoints.
// nolint: unparam
func testTunnelEndpoints(expectIP string, expectPort uint32, adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	cluster := "outbound|8080||eds.test.svc.cluster.local"
	allClusters := adsc.GetEndpoints()
	cla, f := allClusters[cluster]
	if !f || len(cla.Endpoints) == 0 {
		t.Fatalf("No lb endpoints for %v, %v", cluster, adsc.EndpointsJSON())
	}
	var found []string
	for _, lbe := range cla.Endpoints {
		for _, e := range lbe.LbEndpoints {
			addr := e.GetEndpoint().Address.GetSocketAddress().Address
			port := e.GetEndpoint().Address.GetSocketAddress().GetPortValue()
			found = append(found, fmt.Sprintf("%s:%d", addr, port))
			if expectIP == addr && expectPort == port {
				return
			}
		}
	}
	t.Errorf("REACH HERE cannot find %s:%d", expectIP, expectPort)
	t.Fatalf("Expecting address %s:%d got %v", expectIP, expectPort, found)
}

func testLocalityPrioritizedEndpoints(adsc *adsc.ADSC, adsc2 *adsc.ADSC, t *testing.T) {
	endpoints1 := adsc.GetEndpoints()
	endpoints2 := adsc2.GetEndpoints()

	verifyLocalityPriorities(asdcLocality, endpoints1["outbound|80||locality.cluster.local"].GetEndpoints(), t)
	verifyLocalityPriorities(asdc2Locality, endpoints2["outbound|80||locality.cluster.local"].GetEndpoints(), t)

	// No outlier detection specified for this cluster, so we shouldn't apply priority.
	verifyNoLocalityPriorities(endpoints1["outbound|80||locality-no-outlier-detection.cluster.local"].GetEndpoints(), t)
	verifyNoLocalityPriorities(endpoints2["outbound|80||locality-no-outlier-detection.cluster.local"].GetEndpoints(), t)
}

// Tests that Services with multiple ports sharing the same port number are properly sent endpoints.
// Real world use case for this is kube-dns, which uses port 53 for TCP and UDP.
func testOverlappingPorts(s *xds.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// Test initial state
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)

	s.Discovery.Push(&model.PushRequest{
		Full: true,
		ConfigsUpdated: map[model.ConfigKey]struct{}{{
			Kind: gvk.ServiceEntry,
			Name: "overlapping.cluster.local",
		}: {}},
	})
	_, _ = adsc.Wait(5 * time.Second)

	// After the incremental push, we should still see the endpoint
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)
}

func verifyNoLocalityPriorities(eps []*endpoint.LocalityLbEndpoints, t *testing.T) {
	for _, ep := range eps {
		if ep.GetPriority() != 0 {
			t.Errorf("expected no locality priorities to apply, got priority %v.", ep.GetPriority())
		}
	}
}

func verifyLocalityPriorities(proxyLocality string, eps []*endpoint.LocalityLbEndpoints, t *testing.T) {
	items := strings.SplitN(proxyLocality, "/", 3)
	region, zone, subzone := items[0], items[1], items[2]
	for _, ep := range eps {
		if ep.GetLocality().Region == region {
			if ep.GetLocality().Zone == zone {
				if ep.GetLocality().SubZone == subzone {
					if ep.GetPriority() != 0 {
						t.Errorf("expected endpoint pool from same locality to have priority of 0, got %v", ep.GetPriority())
					}
				} else if ep.GetPriority() != 1 {
					t.Errorf("expected endpoint pool from a different subzone to have priority of 1, got %v", ep.GetPriority())
				}
			} else {
				if ep.GetPriority() != 2 {
					t.Errorf("expected endpoint pool from a different zone to have priority of 2, got %v", ep.GetPriority())
				}
			}
		} else {
			if ep.GetPriority() != 3 {
				t.Errorf("expected endpoint pool from a different region to have priority of 3, got %v", ep.GetPriority())
			}
		}
	}
}

// Verify server sends UDS endpoints
func testUdsEndpoints(adsc *adsc.ADSC, t *testing.T) {
	// Check the UDS endpoint ( used to be separate test - but using old unused GRPC method)
	// The new test also verifies CDS is pusing the UDS cluster, since adsc.eds is
	// populated using CDS response
	lbe, f := adsc.GetEndpoints()["outbound|0||localuds.cluster.local"]
	if !f || len(lbe.Endpoints) == 0 {
		t.Error("No UDS lb endpoints")
	} else {
		ep0 := lbe.Endpoints[0]
		if len(ep0.LbEndpoints) != 1 {
			t.Fatalf("expected 1 LB endpoint but got %d", len(ep0.LbEndpoints))
		}
		lbep := ep0.LbEndpoints[0]
		path := lbep.GetEndpoint().GetAddress().GetPipe().GetPath()
		if path != udsPath {
			t.Fatalf("expected Pipe to %s, got %s", udsPath, path)
		}
	}
}

// Update
func edsUpdates(s *xds.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// Old style (non-incremental)
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.3", "hello-sa", "v1"))

	xds.AdsPushAll(s.Discovery)

	// will trigger recompute and push

	if _, err := adsc.Wait(5*time.Second, v3.EndpointType); err != nil {
		t.Fatal("EDS push failed", err)
	}
	testTCPEndpoints("127.0.0.3", adsc, t)
}

// edsFullUpdateCheck checks for updates required in a full push after the CDS update
func edsFullUpdateCheck(adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	if upd, err := adsc.Wait(15*time.Second, watchAll...); err != nil {
		t.Fatal("Expecting CDS, EDS, LDS, and RDS update as part of a full push", err, upd)
	}
}

// This test must be run in isolation, can't be parallelized with any other v2 test.
// It makes different kind of updates, and checks that incremental or full push happens.
// In particular:
// - just endpoint changes -> incremental
// - service account changes -> full ( in future: CDS only )
// - label changes -> full
func edsUpdateInc(s *xds.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// TODO: set endpoints for a different cluster (new shard)

	// Verify initial state
	testTCPEndpoints("127.0.0.1", adsc, t)

	adsc.WaitClear() // make sure there are no pending pushes.

	// Equivalent with the event generated by K8S watching the Service.
	// Will trigger a push.
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))

	upd, err := adsc.Wait(5*time.Second, v3.EndpointType)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if contains(upd, v3.ClusterType) {
		t.Fatal("Expecting EDS only update, got", upd)
	}

	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint with different SA - expect full
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "account2", "v1"))

	edsFullUpdateCheck(adsc, t)
	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint again, no SA change - expect incremental
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.4", "account2", "v1"))

	upd, err = adsc.Wait(5 * time.Second)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if !reflect.DeepEqual(upd, []string{v3.EndpointType}) {
		t.Fatal("Expecting EDS only update, got", upd)
	}
	testTCPEndpoints("127.0.0.4", adsc, t)

	// Update the endpoint to original SA - expect full
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
	edsFullUpdateCheck(adsc, t)
	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint again, no label change - expect incremental
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.5", "hello-sa", "v1"))

	upd, err = adsc.Wait(5 * time.Second)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if !reflect.DeepEqual(upd, []string{v3.EndpointType}) {
		t.Fatal("Expecting EDS only update, got", upd)
	}
	testTCPEndpoints("127.0.0.5", adsc, t)

	// Wipe out all endpoints - expect full
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "", []*model.IstioEndpoint{})

	if upd, err := adsc.Wait(15*time.Second, v3.EndpointType); err != nil {
		t.Fatal("Expecting EDS update as part of a partial push", err, upd)
	}

	lbe := adsc.GetEndpoints()["outbound|8080||eds.test.svc.cluster.local"]
	if len(lbe.Endpoints) != 0 {
		t.Fatalf("There should be no endpoints for outbound|8080||eds.test.svc.cluster.local. Endpoints:\n%v", adsc.EndpointsJSON())
	}
}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
// This test includes a 'bad client' regression test, which fails to read on the
// stream.
func multipleRequest(s *xds.FakeDiscoveryServer, inc bool, nclients,
	nPushes int, to time.Duration, _ map[string]string, t *testing.T) {
	wgConnect := &sync.WaitGroup{}
	wg := &sync.WaitGroup{}
	errChan := make(chan error, nclients)

	// Bad client - will not read any response. This triggers Write to block, which should
	// be detected
	// This is not using adsc, which consumes the events automatically.
	ads := s.ConnectADS()
	ads.Request(t, nil)

	n := nclients
	wg.Add(n)
	wgConnect.Add(n)
	rcvPush := uatomic.NewInt32(0)
	rcvClients := uatomic.NewInt32(0)
	for i := 0; i < n; i++ {
		current := i
		go func(id int) {
			defer wg.Done()
			// Connect and get initial response
			adscConn := s.Connect(&model.Proxy{IPAddresses: []string{fmt.Sprintf("1.1.1.%d", id)}}, nil, nil)
			_, err := adscConn.Wait(15*time.Second, v3.RouteType)
			if err != nil {
				errChan <- errors.New("failed to get initial rds: " + err.Error())
				wgConnect.Done()
				return
			}

			if len(adscConn.GetEndpoints()) == 0 {
				errChan <- errors.New("no endpoints")
				wgConnect.Done()
				return
			}

			wgConnect.Done()

			// Check we received all pushes
			log.Info("Waiting for pushes ", id)

			// Pushes may be merged so we may not get nPushes pushes
			got, err := adscConn.Wait(15*time.Second, v3.EndpointType)

			// If in incremental mode, shouldn't receive cds|rds|lds here
			if inc {
				for _, g := range got {
					if g == "cds" || g == "rds" || g == "lds" {
						errChan <- fmt.Errorf("should be eds incremental but received cds. %v %v",
							err, id)
						return
					}
				}
			}

			rcvPush.Inc()
			if err != nil {
				log.Info("Recv failed", err, id)
				errChan <- fmt.Errorf("failed to receive a response in 15 s %v %v",
					err, id)
				return
			}

			log.Info("Received all pushes ", id)
			rcvClients.Inc()

			adscConn.Close()
		}(current)
	}
	ok := waitTimeout(wgConnect, to)
	if !ok {
		t.Fatal("Failed to connect")
	}
	log.Info("Done connecting")

	// All clients are connected - this can start pushing changes.
	for j := 0; j < nPushes; j++ {
		if inc {
			// This will be throttled - we want to trigger a single push
			s.Discovery.AdsPushAll(strconv.Itoa(j), &model.PushRequest{
				Full: false,
				ConfigsUpdated: map[model.ConfigKey]struct{}{{
					Kind: gvk.ServiceEntry,
					Name: edsIncSvc,
				}: {}},
				Push: s.Discovery.Env.PushContext,
			})
		} else {
			xds.AdsPushAll(s.Discovery)
		}
		log.Info("Push done ", j)
	}

	ok = waitTimeout(wg, to)
	if !ok {
		t.Errorf("Failed to receive all responses %d %d", rcvClients.Load(), rcvPush.Load())
		buf := make([]byte, 1<<16)
		runtime.Stack(buf, true)
		fmt.Printf("%s", buf)
	}

	close(errChan)

	// moved from ads_test, which had a duplicated test.
	for e := range errChan {
		t.Error(e)
	}
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

const udsPath = "/var/run/test/socket"

func addUdsEndpoint(s *xds.DiscoveryServer) {
	s.MemRegistry.AddService(&model.Service{
		Hostname: "localuds.cluster.local",
		Ports: model.PortList{
			{
				Name:     "grpc",
				Port:     0,
				Protocol: protocol.GRPC,
			},
		},
		MeshExternal: true,
		Resolution:   model.ClientSideLB,
	})
	s.MemRegistry.AddInstance("localuds.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         udsPath,
			EndpointPort:    0,
			ServicePortName: "grpc",
			Locality:        model.Locality{Label: "localhost"},
			Labels:          map[string]string{"socket": "unix"},
		},
		ServicePort: &model.Port{
			Name:     "grpc",
			Port:     0,
			Protocol: protocol.GRPC,
		},
	})

	pushReq := &model.PushRequest{
		Full:   true,
		Reason: []model.TriggerReason{model.ConfigUpdate},
	}
	s.ConfigUpdate(pushReq)
}

func addLocalityEndpoints(server *xds.DiscoveryServer, hostname host.Name) {
	server.MemRegistry.AddService(&model.Service{
		Hostname: hostname,
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		},
	})
	localities := []string{
		"region1/zone1/subzone1",
		"region1/zone1/subzone2",
		"region1/zone2/subzone1",
		"region2/zone1/subzone1",
		"region2/zone1/subzone2",
		"region2/zone2/subzone1",
		"region2/zone2/subzone2",
	}
	for i, locality := range localities {
		server.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         fmt.Sprintf("10.0.0.%v", i),
				EndpointPort:    80,
				ServicePortName: "http",
				Locality:        model.Locality{Label: locality},
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
	}
}

// nolint: unparam
func addEdsCluster(s *xds.FakeDiscoveryServer, hostName string, portName string, address string, port int) {
	s.Discovery.MemRegistry.AddService(&model.Service{
		Hostname: host.Name(hostName),
		Ports: model.PortList{
			{
				Name:     portName,
				Port:     port,
				Protocol: protocol.HTTP,
			},
		},
	})

	s.Discovery.MemRegistry.AddInstance(host.Name(hostName), &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         address,
			EndpointPort:    uint32(port),
			ServicePortName: portName,
		},
		ServicePort: &model.Port{
			Name:     portName,
			Port:     port,
			Protocol: protocol.HTTP,
		},
	})
	fullPush(s)
}

func updateServiceResolution(s *xds.FakeDiscoveryServer, resolution model.Resolution) {
	s.Discovery.MemRegistry.AddService(&model.Service{
		Hostname: "edsdns.svc.cluster.local",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: resolution,
	})

	s.Discovery.MemRegistry.AddInstance("edsdns.svc.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "somevip.com",
			EndpointPort:    8080,
			ServicePortName: "http",
		},
		ServicePort: &model.Port{
			Name:     "http",
			Port:     8080,
			Protocol: protocol.HTTP,
		},
	})

	fullPush(s)
}

func addOverlappingEndpoints(s *xds.FakeDiscoveryServer) {
	s.Discovery.MemRegistry.AddService(&model.Service{
		Hostname: "overlapping.cluster.local",
		Ports: model.PortList{
			{
				Name:     "dns",
				Port:     53,
				Protocol: protocol.UDP,
			},
			{
				Name:     "tcp-dns",
				Port:     53,
				Protocol: protocol.TCP,
			},
		},
	})
	s.Discovery.MemRegistry.AddInstance("overlapping.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.0.0.53",
			EndpointPort:    53,
			ServicePortName: "tcp-dns",
		},
		ServicePort: &model.Port{
			Name:     "tcp-dns",
			Port:     53,
			Protocol: protocol.TCP,
		},
	})
	fullPush(s)
}

func addUnhealthyCluster(s *xds.FakeDiscoveryServer) {
	s.Discovery.MemRegistry.AddService(&model.Service{
		Hostname: "unhealthy.svc.cluster.local",
		Ports: model.PortList{
			{
				Name:     "tcp-dns",
				Port:     53,
				Protocol: protocol.TCP,
			},
		},
	})
	s.Discovery.MemRegistry.AddInstance("unhealthy.svc.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.0.0.53",
			EndpointPort:    53,
			ServicePortName: "tcp-dns",
			HealthStatus:    model.UnHealthy,
		},
		ServicePort: &model.Port{
			Name:     "tcp-dns",
			Port:     53,
			Protocol: protocol.TCP,
		},
	})
	fullPush(s)
}

// Verify the endpoint debug interface is installed and returns some string.
// TODO: parse response, check if data captured matches what we expect.
// TODO: use this in integration tests.
// TODO: refine the output
// TODO: dump the ServiceInstances as well
func testEdsz(t *testing.T, s *xds.FakeDiscoveryServer, proxyID string) {
	req, err := http.NewRequest("GET", "/debug/edsz?proxyID="+proxyID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	debug := http.HandlerFunc(s.Discovery.Edsz)
	debug.ServeHTTP(rr, req)

	data, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("Failed to read /edsz")
	}
	statusStr := string(data)

	if !strings.Contains(statusStr, "\"outbound|8080||eds.test.svc.cluster.local\"") {
		t.Fatal("Mock eds service not found ", statusStr)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
