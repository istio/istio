// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package xds_test

import (
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
	"strings"
	"sync"
	"testing"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	uatomic "go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	xdsfake "istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
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
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-all.yaml") +
			mustReadFile(t, "tests/testdata/config/static-weighted-se.yaml") +
			mustReadFile(t, "tests/testdata/config/peer-authn-strict.yaml"),
	})
	ads := s.Connect(nil, nil, watchAll)
	t.Run("Full Push", func(t *testing.T) {
		s.Discovery.Push(&model.PushRequest{Forced: true})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Incremental Push with updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "destall.default.svc.cluster.local", Namespace: "testns"}),
		})
		if err := ads.WaitSingle(time.Second*5, v3.EndpointType, v3.ClusterType); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Full Push with updated services", func(t *testing.T) {
		ads.WaitClear()

		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "weighted.static.svc.cluster.local", Namespace: "default"}),
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 1 {
			t.Fatalf("Expected a partial EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push with updated unrelated services", func(t *testing.T) {
		ads.WaitClear()

		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "weighted.static.svc.cluster.local", Namespace: "otherns"}),
		})
		upd, _ := ads.Wait(time.Millisecond*100, watchAll...)
		if slices.Contains(upd, v3.EndpointType) {
			t.Fatalf("Expected no EDS push, got %v", upd)
		}
	})
	t.Run("Full Push with updated services and virtual services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "weighted.static.svc.cluster.local", Namespace: "default"},
				model.ConfigKey{Kind: kind.VirtualService, Name: "vs", Namespace: "testns"},
			),
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 1 {
			t.Fatalf("Expected a partial EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push with updated destination rules", func(t *testing.T) {
		ads.WaitClear()
		fmt.Println("start test with drs update")
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: "destall", Namespace: "testns"}),
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 3 {
			t.Fatalf("Expected an incremental EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push with updated unrelated destination rules", func(t *testing.T) {
		ads.WaitClear()
		fmt.Println("start test with drs update")
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: "destall", Namespace: "otherns"}),
		})
		upd, _ := ads.Wait(time.Millisecond*100, watchAll...)
		if slices.Contains(upd, v3.EndpointType) {
			t.Fatalf("Expected no EDS push, got %v", upd)
		}
	})
	t.Run("Full Push with updated services and destination rules", func(t *testing.T) {
		ads.WaitClear()
		fmt.Println("start test with drs update")
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "destall.default.svc.cluster.local", Namespace: "testns"},
				model.ConfigKey{Kind: kind.DestinationRule, Name: "destall", Namespace: "testns"}),
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 3 {
			t.Fatalf("Expected an incremental EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push with multiple updates", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "destall.default.svc.cluster.local", Namespace: "default"},
				model.ConfigKey{Kind: kind.VirtualService, Name: "vs", Namespace: "testns"},
				model.ConfigKey{Kind: kind.DestinationRule, Name: "destall", Namespace: "testns"}),
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 3 {
			t.Fatalf("Expected an incremental EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push without updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "destall", Namespace: "testns"}),
		})
		if _, err := ads.Wait(time.Second*5, v3.ClusterType, v3.EndpointType); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) < 3 {
			t.Fatalf("Expected a full EDS update, but got: %v", ads.GetEndpoints())
		}
	})
	t.Run("Full Push with updated peer authentication", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.PeerAuthentication, Name: "default", Namespace: "testns"}),
		})
		if _, err := ads.Wait(time.Second*5, v3.ClusterType, v3.EndpointType); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) != 3 {
			t.Fatalf("Expected a partial EDS update, but got: %v", xdstest.MapKeys(ads.GetEndpoints()))
		}
	})
	t.Run("Full Push with updated unrelated peer authentication", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.PeerAuthentication, Name: "default", Namespace: "otherns"}),
		})
		upd, _ := ads.Wait(time.Second*5, watchAll...)
		if slices.Contains(upd, v3.EndpointType) {
			t.Fatalf("Expected no EDS push, got %v", upd)
		}
	})
}

// Regression test for https://github.com/istio/istio/issues/38709
func TestSAUpdate(t *testing.T) {
	test.SetAtomicBoolForTest(t, features.GlobalSendUnhealthyEndpoints, false)
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.Connect(s.SetupProxy(nil), nil, []string{v3.ClusterType})

	ports := model.PortList{
		{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		},
	}
	svc := &model.Service{
		Ports:    ports,
		Hostname: host.Name("test1"),
	}
	s.MemRegistry.AddService(svc)
	if _, err := ads.Wait(time.Second*10, watchAll...); err != nil {
		t.Fatal(err)
	}
	i := &model.ServiceInstance{
		Service:     svc,
		ServicePort: svc.Ports[0],
		Endpoint: &model.IstioEndpoint{
			Addresses:      []string{"1.2.3.4"},
			ServiceAccount: "spiffe://td1/ns/def/sa/def",
			HealthStatus:   model.UnHealthy,
		},
	}
	s.MemRegistry.AddInstance(i)
	if _, err := ads.Wait(time.Second*10, v3.EndpointType); err != nil {
		t.Fatal(err)
	}
	transport := &tls.UpstreamTlsContext{}
	ads.GetEdsClusters()["outbound|80||test1"].GetTransportSocketMatches()[0].GetTransportSocket().GetTypedConfig().UnmarshalTo(transport)
	sans := transport.GetCommonTlsContext().GetCombinedValidationContext().GetDefaultValidationContext().GetMatchSubjectAltNames() //nolint: staticcheck
	if len(sans) != 1 {
		t.Fatalf("expected 1 san, got %v", sans)
	}
}

// Regression test for https://github.com/istio/istio/issues/38709
func TestSAUpdateWithMulAddrsInstance(t *testing.T) {
	test.SetAtomicBoolForTest(t, features.GlobalSendUnhealthyEndpoints, false)
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.Connect(s.SetupProxy(nil), nil, []string{v3.ClusterType})

	ports := model.PortList{
		{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		},
	}
	svc := &model.Service{
		Ports:    ports,
		Hostname: host.Name("test1"),
	}
	s.MemRegistry.AddService(svc)
	if _, err := ads.Wait(time.Second*10, watchAll...); err != nil {
		t.Fatal(err)
	}
	i := &model.ServiceInstance{
		Service:     svc,
		ServicePort: svc.Ports[0],
		Endpoint: &model.IstioEndpoint{
			Addresses:      []string{"1.2.3.4", "2001:1::1"},
			ServiceAccount: "spiffe://td1/ns/def/sa/def",
			HealthStatus:   model.UnHealthy,
		},
	}
	s.MemRegistry.AddInstance(i)
	if _, err := ads.Wait(time.Second*10, v3.EndpointType); err != nil {
		t.Fatal(err)
	}
	transport := &tls.UpstreamTlsContext{}
	ads.GetEdsClusters()["outbound|80||test1"].GetTransportSocketMatches()[0].GetTransportSocket().GetTypedConfig().UnmarshalTo(transport)
	sans := transport.GetCommonTlsContext().GetCombinedValidationContext().GetDefaultValidationContext().GetMatchSubjectAltNames() //nolint: staticcheck
	if len(sans) != 1 {
		t.Fatalf("expected 1 san, got %v", sans)
	}
}

func TestEds(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-locality.yaml"),
	})

	m := s.MemRegistry
	addUdsEndpoint(s.Discovery, m)

	// enable locality load balancing and add relevant endpoints in order to test
	addLocalityEndpoints(m, "locality.cluster.local")
	addLocalityEndpoints(m, "locality-no-outlier-detection.cluster.local")

	// Add the test ads clients to list of service instances in order to test the context dependent locality coloring.
	addTestClientEndpoints(m)

	m.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	m.SetEndpoints(edsIncSvc, "", newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))
	// Let initial updates settle
	s.EnsureSynced(t)

	adscConn := s.Connect(&model.Proxy{Locality: util.ConvertLocality(asdcLocality), IPAddresses: []string{"10.10.10.10"}}, nil, watchAll)
	adscConn2 := s.Connect(&model.Proxy{Locality: util.ConvertLocality(asdc2Locality), IPAddresses: []string{"10.10.10.11"}}, nil, watchAll)

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
	})
}

// newEndpointWithAccount is a helper for IstioEndpoint creation. Creates endpoints with
// port name "http", with the given IP, service account and a 'version' label.
// nolint: unparam
func newEndpointWithAccount(ip, account, version string) []*model.IstioEndpoint {
	return []*model.IstioEndpoint{
		{
			Addresses:       []string{ip},
			ServicePortName: "http-main",
			EndpointPort:    80,
			Labels:          map[string]string{"version": version},
			ServiceAccount:  account,
		},
	}
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
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{ConfigString: mustReadFile(t, "tests/testdata/config/static-weighted-se.yaml")})
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
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	addOverlappingEndpoints(s)
	adscon := s.Connect(nil, nil, watchEds)
	testOverlappingPorts(s, adscon, t)
}

func TestEDSUnhealthyEndpoints(t *testing.T) {
	for _, sendUnhealthy := range []bool{true, false} {
		t.Run(fmt.Sprint(sendUnhealthy), func(t *testing.T) {
			test.SetAtomicBoolForTest(t, features.GlobalSendUnhealthyEndpoints, sendUnhealthy)
			s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
			addUnhealthyCluster(s, sendUnhealthy)
			s.EnsureSynced(t)
			adscon := s.Connect(nil, nil, watchEds)

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
					upd, err := adscon.Wait(5*time.Second, v3.EndpointType)
					assert.NoError(t, err)
					if len(upd) > 0 && !slices.Contains(upd, v3.EndpointType) {
						t.Fatalf("Expecting EDS push as endpoint health is changed. But received %v", upd)
					}
				} else {
					upd, _ := adscon.Wait(50*time.Millisecond, v3.EndpointType)
					if slices.Contains(upd, v3.EndpointType) {
						t.Fatalf("Expected no EDS push, got %v", upd)
					}
				}

				// Validate that endpoints are pushed.
				lbe := adscon.GetEndpoints()["outbound|53||unhealthy.svc.cluster.local"]
				eh, euh := xdstest.ExtractHealthEndpoints(lbe)
				gotHealthy := sets.SortedList(sets.New(eh...))
				gotUnhealthy := sets.SortedList(sets.New(euh...))
				if !reflect.DeepEqual(gotHealthy, healthy) {
					t.Fatalf("did not get expected endpoints: got %v, want %v", gotHealthy, healthy)
				}
				if !reflect.DeepEqual(gotUnhealthy, unhealthy) {
					t.Fatalf("did not get expected unhealthy endpoints: got %v, want %v", gotUnhealthy, unhealthy)
				}
			}

			// Validate that we do send initial unhealthy endpoints.
			// ExpectPush=false since we are just querying the initial state, we already got the responses in our initial connection
			if sendUnhealthy {
				validateEndpoints(false, nil, []string{"10.0.0.53:53"})
			} else {
				validateEndpoints(false, nil, nil)
			}
			adscon.WaitClear()

			// Set additional unhealthy endpoint and validate Eds update is not triggered.
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.UnHealthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
					{
						Addresses:              []string{"10.0.0.54"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.UnHealthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})

			// Validate that endpoint is pushed.
			if sendUnhealthy {
				validateEndpoints(true, nil, []string{"10.0.0.53:53", "10.0.0.54:53"})
			} else {
				validateEndpoints(false, nil, nil)
			}

			// Change the status of endpoint to Healthy and validate Eds is pushed.
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
					{
						Addresses:              []string{"10.0.0.54"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})

			// Validate that endpoints are pushed.
			validateEndpoints(true, []string{"10.0.0.53:53", "10.0.0.54:53"}, nil)

			// Set to exact same endpoints
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
					{
						Addresses:              []string{"10.0.0.54"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})
			// Validate that endpoint is not pushed.
			validateEndpoints(false, []string{"10.0.0.53:53", "10.0.0.54:53"}, nil)

			// Now change the status of endpoint to UnHealthy and validate Eds is pushed.
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.UnHealthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
					{
						Addresses:              []string{"10.0.0.54"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})

			// Validate that endpoints are pushed.
			if sendUnhealthy {
				validateEndpoints(true, []string{"10.0.0.54:53"}, []string{"10.0.0.53:53"})
			} else {
				validateEndpoints(true, []string{"10.0.0.54:53"}, nil)
			}

			// Change the status of endpoint to Healthy and validate Eds is pushed.
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
					{
						Addresses:              []string{"10.0.0.54"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})

			validateEndpoints(true, []string{"10.0.0.54:53", "10.0.0.53:53"}, nil)

			// Remove a healthy endpoint
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})

			validateEndpoints(true, []string{"10.0.0.53:53"}, nil)

			// Add another healthy endpoint and validate Eds is pushed.
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:              []string{"10.0.0.53"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
					{
						Addresses:              []string{"10.0.0.54"},
						EndpointPort:           53,
						ServicePortName:        "tcp-dns",
						HealthStatus:           model.Healthy,
						SendUnhealthyEndpoints: sendUnhealthy,
					},
				})

			// Validate that endpoints are pushed.
			validateEndpoints(true, []string{"10.0.0.53:53", "10.0.0.54:53"}, nil)

			// Mark an endpoint as terminating, it should be removed
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "",
				[]*model.IstioEndpoint{
					{
						Addresses:       []string{"10.0.0.53"},
						EndpointPort:    53,
						ServicePortName: "tcp-dns",
						HealthStatus:    model.Healthy,
					},
					{
						Addresses:       []string{"10.0.0.54"},
						EndpointPort:    53,
						ServicePortName: "tcp-dns",
						HealthStatus:    model.Terminating,
					},
				})

			// Validate that endpoints are pushed.
			validateEndpoints(true, []string{"10.0.0.53:53"}, nil)

			// Remove last healthy endpoints
			s.MemRegistry.SetEndpoints("unhealthy.svc.cluster.local", "", []*model.IstioEndpoint{})
			validateEndpoints(true, nil, nil)
		})
	}
}

// Validates the behavior when Service resolution type is updated after initial EDS push.
// See https://github.com/istio/istio/issues/18355 for more details.
func TestEDSServiceResolutionUpdate(t *testing.T) {
	for _, resolution := range []model.Resolution{model.DNSLB, model.DNSRoundRobinLB} {
		t.Run(fmt.Sprintf("resolution_%s", resolution), func(t *testing.T) {
			s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
			addEdsCluster(s, "edsdns.svc.cluster.local", "http", "10.0.0.53", 8080)
			addEdsCluster(s, "other.local", "http", "1.1.1.1", 8080)
			s.EnsureSynced(t) // Wait for debounce

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
	cases := []struct {
		name           string
		newSa          string
		expectFullPush bool
	}{
		{
			name:           "same service account",
			newSa:          "sa",
			expectFullPush: false,
		},
		{
			name:           "different service account",
			newSa:          "new-sa",
			expectFullPush: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
			addEdsCluster(s, "flipflop.com", "http", "10.0.0.53", 8080)
			s.EnsureSynced(t) // Wait for debounce
			adscConn := s.Connect(nil, nil, watchAll)

			// Validate that endpoints are pushed correctly.
			testEndpoints("10.0.0.53", "outbound|8080||flipflop.com", adscConn, t)

			// Clear the endpoint and validate it does not trigger a full push.
			s.MemRegistry.SetEndpoints("flipflop.com", "", []*model.IstioEndpoint{})

			upd, _ := adscConn.Wait(5*time.Second, v3.EndpointType)
			assert.Equal(t, upd, []string{v3.EndpointType}, "expected partial push")

			lbe := adscConn.GetEndpoints()["outbound|8080||flipflop.com"]
			if len(lbe.Endpoints) != 0 {
				t.Fatalf("There should be no endpoints for outbound|8080||flipflop.com. Endpoints:\n%v", adscConn.EndpointsJSON())
			}

			// Validate that keys in service still exist in EndpointIndex - this prevents full push.
			if _, ok := s.Discovery.Env.EndpointIndex.ShardsForService("flipflop.com", ""); !ok {
				t.Fatalf("Expected service key %s to be present in EndpointIndex. But missing %v", "flipflop.com", s.Discovery.Env.EndpointIndex.Shardz())
			}

			// Set the endpoints again and validate it does not trigger full push.
			s.MemRegistry.SetEndpoints("flipflop.com", "",
				[]*model.IstioEndpoint{
					{
						Addresses:       []string{"10.10.1.1"},
						ServicePortName: "http",
						EndpointPort:    8080,
						ServiceAccount:  tt.newSa,
					},
				})

			upd, _ = adscConn.Wait(5*time.Second, v3.EndpointType)

			if tt.expectFullPush {
				if !slices.Contains(upd, v3.ClusterType) {
					t.Fatalf("expected a CDS push, got: %+v", upd)
				}

				if !slices.Contains(upd, v3.EndpointType) {
					t.Fatalf("expected an EDS push, got: %+v", upd)
				}
			} else {
				if slices.Contains(upd, v3.ClusterType) {
					t.Fatalf("expected no CDS push, got: %+v", upd)
				}

				if !slices.Contains(upd, v3.EndpointType) {
					t.Fatalf("expected an EDS push, got: %+v", upd)
				}
			}

			testEndpoints("10.10.1.1", "outbound|8080||flipflop.com", adscConn, t)
			if shard, ok := s.Discovery.Env.EndpointIndex.ShardsForService("flipflop.com", ""); !ok {
				t.Fatalf("Expected service key %s to be present in EndpointIndex. But missing %v", "flipflop.com", s.Discovery.Env.EndpointIndex.Shardz())
			} else {
				assert.Equal(t, sets.SortedList(shard.ServiceAccounts), []string{tt.newSa})
			}
		})
	}
}

// Validate that deleting a service clears entries from EndpointIndex.
func TestDeleteService(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	addEdsCluster(s, "removeservice.com", "http", "10.0.0.53", 8080)
	adscConn := s.Connect(nil, nil, watchEds)

	// Validate that endpoints are pushed correctly.
	testEndpoints("10.0.0.53", "outbound|8080||removeservice.com", adscConn, t)

	s.MemRegistry.RemoveService("removeservice.com")

	if _, ok := s.Discovery.Env.EndpointIndex.ShardsForService("removeservice.com", ""); ok {
		t.Fatalf("Expected service key %s to be deleted in EndpointIndex. But is still there %v",
			"removeservice.com", s.Discovery.Env.EndpointIndex.Shardz())
	}
}

func fullPush(s *xdsfake.FakeDiscoveryServer) {
	s.Discovery.Push(&model.PushRequest{Forced: true})
}

func addTestClientEndpoints(m *memory.ServiceDiscovery) {
	svc := &model.Service{
		Hostname: "test-1.default",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		},
	}
	m.AddService(svc)
	m.AddInstance(&model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{"10.10.10.10"},
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
	m.AddInstance(&model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{"10.10.10.11"},
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
func testOverlappingPorts(s *xdsfake.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// Test initial state
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)

	s.Discovery.Push(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{
			Kind: kind.ServiceEntry,
			Name: "overlapping.cluster.local",
		}),
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
func edsUpdates(s *xdsfake.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// Old style (non-incremental)
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.3", "hello-sa", "v1"))

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
func edsUpdateInc(s *xdsfake.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// TODO: set endpoints for a different cluster (new shard)

	// Verify initial state
	testTCPEndpoints("127.0.0.1", adsc, t)

	adsc.WaitClear() // make sure there are no pending pushes.

	// Equivalent with the event generated by K8S watching the Service.
	// Will trigger a push.
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))

	upd, err := adsc.Wait(5*time.Second, v3.EndpointType)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if slices.Contains(upd, v3.ClusterType) {
		t.Fatal("Expecting EDS only update, got", upd)
	}

	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint with different SA - expect full
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "account2", "v1"))

	edsFullUpdateCheck(adsc, t)
	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint again, no SA change - expect incremental
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
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
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
	edsFullUpdateCheck(adsc, t)
	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint again, no label change - expect incremental
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
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
	s.MemRegistry.SetEndpoints(edsIncSvc, "", []*model.IstioEndpoint{})

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
func multipleRequest(s *xdsfake.FakeDiscoveryServer, inc bool, nclients,
	nPushes int, to time.Duration, _ map[string]string, t *testing.T,
) {
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
			log.Infof("Waiting for pushes %v", id)

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
				log.Infof("Recv %v failed: %v", id, err)
				errChan <- fmt.Errorf("failed to receive a response in 15 s %v %v",
					err, id)
				return
			}

			log.Infof("Received all pushes %v", id)
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
			s.Discovery.AdsPushAll(&model.PushRequest{
				ConfigsUpdated: sets.New(model.ConfigKey{
					Kind: kind.Endpoints,
					Name: edsIncSvc,
				}),
				Push: s.Discovery.Env.PushContext(),
			})
		} else {
			xds.AdsPushAll(s.Discovery)
		}
		log.Infof("Push %v done", j)
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

func addUdsEndpoint(s *xds.DiscoveryServer, m *memory.ServiceDiscovery) {
	svc := &model.Service{
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
	}
	m.AddService(svc)
	m.AddInstance(&model.ServiceInstance{
		Service: &model.Service{
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
		},
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{udsPath},
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
		Reason: model.NewReasonStats(model.ConfigUpdate),
		Forced: true,
	}
	s.ConfigUpdate(pushReq)
}

func addLocalityEndpoints(m *memory.ServiceDiscovery, hostname host.Name) {
	svc := &model.Service{
		Hostname: hostname,
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		},
	}
	m.AddService(svc)
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
		_, _ = i, locality
		m.AddInstance(&model.ServiceInstance{
			Service: svc,
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{fmt.Sprintf("10.0.0.%v", i)},
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
func addEdsCluster(s *xdsfake.FakeDiscoveryServer, hostName string, portName string, address string, port int) {
	svc := &model.Service{
		Hostname: host.Name(hostName),
		Ports: model.PortList{
			{
				Name:     portName,
				Port:     port,
				Protocol: protocol.HTTP,
			},
		},
	}
	s.MemRegistry.AddService(svc)

	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{address},
			EndpointPort:    uint32(port),
			ServicePortName: portName,
			ServiceAccount:  "sa",
		},
		ServicePort: &model.Port{
			Name:     portName,
			Port:     port,
			Protocol: protocol.HTTP,
		},
	})
	fullPush(s)
}

func updateServiceResolution(s *xdsfake.FakeDiscoveryServer, resolution model.Resolution) {
	svc := &model.Service{
		Hostname: "edsdns.svc.cluster.local",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: resolution,
	}
	s.MemRegistry.AddService(svc)

	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{"somevip.com"},
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

func addOverlappingEndpoints(s *xdsfake.FakeDiscoveryServer) {
	svc := &model.Service{
		DefaultAddress: constants.UnspecifiedIP,
		Hostname:       "overlapping.cluster.local",
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
	}
	s.MemRegistry.AddService(svc)
	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{"10.0.0.53"},
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

func addUnhealthyCluster(s *xdsfake.FakeDiscoveryServer, sendUnhealthy bool) {
	svc := &model.Service{
		DefaultAddress: constants.UnspecifiedIP,
		Hostname:       "unhealthy.svc.cluster.local",
		Ports: model.PortList{
			{
				Name:     "tcp-dns",
				Port:     53,
				Protocol: protocol.TCP,
			},
		},
	}
	s.MemRegistry.AddService(svc)
	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Addresses:              []string{"10.0.0.53"},
			EndpointPort:           53,
			ServicePortName:        "tcp-dns",
			HealthStatus:           model.UnHealthy,
			SendUnhealthyEndpoints: sendUnhealthy,
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
func testEdsz(t *testing.T, s *xdsfake.FakeDiscoveryServer, proxyID string) {
	req, err := http.NewRequest(http.MethodGet, "/debug/edsz?proxyID="+proxyID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.DiscoveryDebug.ServeHTTP(rr, req)

	data, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatal("Failed to read /edsz")
	}
	statusStr := string(data)

	if !strings.Contains(statusStr, "\"outbound|8080||eds.test.svc.cluster.local\"") {
		t.Fatal("Mock eds service not found ", statusStr)
	}
}

// TestEdsLocalCluster covers the zone-aware-routing local_cluster EDS path:
//   - The proxy can subscribe to xds.LocalClusterName and receive a CLA built from
//     its own service's endpoints, with locality preserved.
//   - DestinationRule changes for the local service do NOT trigger a recompute of
//     local_cluster (since the DR is intentionally ignored for it).
//   - Endpoint changes to the local service DO trigger a recompute.
func TestEdsLocalCluster(t *testing.T) {
	const (
		proxyIP    = "1.1.1.1"
		siblingIP  = "2.2.2.2"
		svcNS      = "default"
		svcName    = "local"
		svcHost    = "local.default.svc.cluster.local"
		nodeID     = "sidecar~1.1.1.1~test.default~default.svc.cluster.local"
		portName   = "http"
		portNumber = 80
	)

	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	svc := &model.Service{
		Hostname: host.Name(svcHost),
		Ports: model.PortList{{
			Name:     portName,
			Port:     portNumber,
			Protocol: protocol.HTTP,
		}},
		Attributes: model.ServiceAttributes{Namespace: svcNS, Name: svcName},
	}
	s.MemRegistry.AddService(svc)
	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service:     svc,
		ServicePort: svc.Ports[0],
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{proxyIP},
			ServicePortName: portName,
			EndpointPort:    portNumber,
			Locality:        model.Locality{Label: "region1/zone1/subzone1"},
			TLSMode:         model.IstioMutualTLSModeLabel,
			HealthStatus:    model.Healthy,
		},
	})
	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service:     svc,
		ServicePort: svc.Ports[0],
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{siblingIP},
			ServicePortName: portName,
			EndpointPort:    portNumber,
			Locality:        model.Locality{Label: "region1/zone2/subzone2"},
			TLSMode:         model.IstioMutualTLSModeLabel,
			HealthStatus:    model.Healthy,
		},
	})
	s.EnsureSynced(t)

	// Set the proxy locality so filterIstioEndpoint's same-region filter does not
	// reject endpoints. Both proxyIP (region1/zone1) and siblingIP (region1/zone2) are
	// in the same region, so both should appear in the local_cluster CLA.
	ads := s.ConnectADS().
		WithID(nodeID).
		WithType(v3.EndpointType).
		WithMetadata(model.NodeMetadata{
			Namespace: svcNS,
			Labels: map[string]string{
				"topology.kubernetes.io/region": "region1",
				"topology.kubernetes.io/zone":   "zone1",
			},
		})
	resp := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{xds.LocalClusterName},
	})

	t.Run("initial response includes both localities", func(t *testing.T) {
		if len(resp.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
		}
		cla := &endpoint.ClusterLoadAssignment{}
		if err := resp.Resources[0].UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		if cla.ClusterName != xds.LocalClusterName {
			t.Errorf("cluster name = %q, want %q", cla.ClusterName, xds.LocalClusterName)
		}
		got := map[string]string{}
		for _, lle := range cla.Endpoints {
			for _, lbe := range lle.LbEndpoints {
				addr := lbe.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
				got[addr] = lle.Locality.GetZone()
			}
		}
		want := map[string]string{
			proxyIP:   "zone1",
			siblingIP: "zone2",
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("endpoints = %v, want %v", got, want)
		}
	})

	t.Run("DR change does not recompute local_cluster", func(t *testing.T) {
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "any", Namespace: svcNS}),
		})
		// A partial push that doesn't touch local_cluster should not produce a non-empty
		// response for this proxy (it's only watching local_cluster).
		ads.ExpectNoResponse(t)
	})

	t.Run("endpoint change recomputes local_cluster", func(t *testing.T) {
		s.MemRegistry.AddInstance(&model.ServiceInstance{
			Service:     svc,
			ServicePort: svc.Ports[0],
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{"3.3.3.3"},
				ServicePortName: portName,
				EndpointPort:    portNumber,
				Locality:        model.Locality{Label: "region1/zone3/subzone3"},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
		})
		updated := ads.ExpectResponse(t)
		if len(updated.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(updated.Resources))
		}
		cla := &endpoint.ClusterLoadAssignment{}
		if err := updated.Resources[0].UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		zones := map[string]bool{}
		for _, lle := range cla.Endpoints {
			zones[lle.Locality.GetZone()] = true
		}
		if !zones["zone3"] {
			t.Errorf("expected new zone3 endpoint, got zones %v", zones)
		}
	})

	t.Run("unrelated service update does not recompute local_cluster", func(t *testing.T) {
		// A ServiceEntry update for a service that is not the proxy's local service
		// must not produce an EDS response for local_cluster.
		s.Discovery.Push(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      "other.default.svc.cluster.local",
				Namespace: svcNS,
			}),
		})
		ads.ExpectNoResponse(t)
	})

	t.Run("endpoint removal shrinks local_cluster", func(t *testing.T) {
		// Drop the 3.3.3.3 endpoint added by the previous subtest, leaving only
		// the proxy's own endpoint and the original sibling.
		s.MemRegistry.SetEndpoints(svcHost, svcNS, []*model.IstioEndpoint{
			{
				Addresses:       []string{proxyIP},
				ServicePortName: portName,
				EndpointPort:    portNumber,
				Locality:        model.Locality{Label: "region1/zone1/subzone1"},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
			{
				Addresses:       []string{siblingIP},
				ServicePortName: portName,
				EndpointPort:    portNumber,
				Locality:        model.Locality{Label: "region1/zone2/subzone2"},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
		})
		updated := ads.ExpectResponse(t)
		cla := &endpoint.ClusterLoadAssignment{}
		if err := updated.Resources[0].UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		addrs := map[string]bool{}
		for _, lle := range cla.Endpoints {
			for _, lbe := range lle.LbEndpoints {
				addrs[lbe.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()] = true
			}
		}
		want := map[string]bool{proxyIP: true, siblingIP: true}
		if !reflect.DeepEqual(addrs, want) {
			t.Errorf("CLA endpoints = %v, want %v", addrs, want)
		}
	})

	t.Run("cross-region endpoint excluded from local_cluster", func(t *testing.T) {
		// Add an endpoint for the same service in a different region. It must not
		// appear in local_cluster because the same-region filter in filterIstioEndpoint
		// keeps cross-region endpoints out of the zone-aware LB's source cluster.
		const remoteIP = "4.4.4.4"
		s.MemRegistry.AddInstance(&model.ServiceInstance{
			Service:     svc,
			ServicePort: svc.Ports[0],
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{remoteIP},
				ServicePortName: portName,
				EndpointPort:    portNumber,
				Locality:        model.Locality{Label: "otherregion/zone1/subzone1"},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
		})
		updated := ads.ExpectResponse(t)
		cla := &endpoint.ClusterLoadAssignment{}
		if err := updated.Resources[0].UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		for _, lle := range cla.Endpoints {
			for _, lbe := range lle.LbEndpoints {
				addr := lbe.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
				if addr == remoteIP {
					t.Errorf("cross-region endpoint %s should be excluded from local_cluster, but was present", remoteIP)
				}
			}
		}
	})
}

// TestEdsLocalClusterHashFilter verifies that filterIstioEndpoint restricts the
// local_cluster CLA to endpoints whose pod-template-hash / rollouts-pod-template-hash
// labels match the calling proxy's labels when those labels are present.
func TestEdsLocalClusterHashFilter(t *testing.T) {
	const (
		proxyIP    = "1.1.1.1"
		otherIP    = "2.2.2.2"
		svcNS      = "default"
		svcHost    = "local.default.svc.cluster.local"
		nodeID     = "sidecar~1.1.1.1~test.default~default.svc.cluster.local"
		portName   = "http"
		portNumber = 80
		proxyHash  = "abc123"
		otherHash  = "xyz789"
	)

	// topoLabels are always needed: the region filter runs before the hash filter.
	topoLabels := map[string]string{
		"topology.kubernetes.io/region": "region1",
		"topology.kubernetes.io/zone":   "zone1",
	}

	mkEp := func(addr string, extraLabels map[string]string) *model.IstioEndpoint {
		lbls := map[string]string{}
		for k, v := range extraLabels {
			lbls[k] = v
		}
		return &model.IstioEndpoint{
			Addresses:       []string{addr},
			ServicePortName: portName,
			EndpointPort:    portNumber,
			Locality:        model.Locality{Label: "region1/zone1/subzone1"},
			TLSMode:         model.IstioMutualTLSModeLabel,
			HealthStatus:    model.Healthy,
			Labels:          lbls,
		}
	}

	withHash := func(base map[string]string, extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	cases := []struct {
		name        string
		proxyLabels map[string]string
		endpoints   []*model.IstioEndpoint
		wantAddrs   map[string]bool
	}{
		{
			name:        "pod-template-hash: matching endpoint included",
			proxyLabels: withHash(topoLabels, map[string]string{"pod-template-hash": proxyHash}),
			endpoints: []*model.IstioEndpoint{
				mkEp(proxyIP, map[string]string{"pod-template-hash": proxyHash}),
			},
			wantAddrs: map[string]bool{proxyIP: true},
		},
		{
			name:        "pod-template-hash: mismatched endpoint excluded",
			proxyLabels: withHash(topoLabels, map[string]string{"pod-template-hash": proxyHash}),
			endpoints: []*model.IstioEndpoint{
				mkEp(proxyIP, map[string]string{"pod-template-hash": proxyHash}),
				mkEp(otherIP, map[string]string{"pod-template-hash": otherHash}),
			},
			wantAddrs: map[string]bool{proxyIP: true},
		},
		{
			name:        "pod-template-hash: endpoint without label excluded when proxy has hash",
			proxyLabels: withHash(topoLabels, map[string]string{"pod-template-hash": proxyHash}),
			endpoints: []*model.IstioEndpoint{
				mkEp(proxyIP, map[string]string{"pod-template-hash": proxyHash}),
				mkEp(otherIP, nil),
			},
			wantAddrs: map[string]bool{proxyIP: true},
		},
		{
			// When the proxy has no pod-template-hash label the filter is not applied
			// and all same-region endpoints appear in the CLA regardless of their labels.
			name:        "pod-template-hash: no hash on proxy passes all same-region endpoints",
			proxyLabels: topoLabels,
			endpoints: []*model.IstioEndpoint{
				mkEp(proxyIP, nil),
				mkEp(otherIP, nil),
			},
			wantAddrs: map[string]bool{proxyIP: true, otherIP: true},
		},
		{
			name:        "rollouts-pod-template-hash: matching endpoint included, mismatched excluded",
			proxyLabels: withHash(topoLabels, map[string]string{"rollouts-pod-template-hash": proxyHash}),
			endpoints: []*model.IstioEndpoint{
				mkEp(proxyIP, map[string]string{"rollouts-pod-template-hash": proxyHash}),
				mkEp(otherIP, map[string]string{"rollouts-pod-template-hash": otherHash}),
			},
			wantAddrs: map[string]bool{proxyIP: true},
		},
		{
			name:        "rollouts-pod-template-hash: endpoint without label excluded when proxy has hash",
			proxyLabels: withHash(topoLabels, map[string]string{"rollouts-pod-template-hash": proxyHash}),
			endpoints: []*model.IstioEndpoint{
				mkEp(proxyIP, map[string]string{"rollouts-pod-template-hash": proxyHash}),
				mkEp(otherIP, nil),
			},
			wantAddrs: map[string]bool{proxyIP: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
			svc := &model.Service{
				Hostname: host.Name(svcHost),
				Ports: model.PortList{{
					Name:     portName,
					Port:     portNumber,
					Protocol: protocol.HTTP,
				}},
				Attributes: model.ServiceAttributes{Namespace: svcNS, Name: "local"},
			}
			s.MemRegistry.AddService(svc)
			for _, ep := range tc.endpoints {
				s.MemRegistry.AddInstance(&model.ServiceInstance{
					Service:     svc,
					ServicePort: svc.Ports[0],
					Endpoint:    ep,
				})
			}
			s.EnsureSynced(t)

			ads := s.ConnectADS().
				WithID(nodeID).
				WithType(v3.EndpointType).
				WithMetadata(model.NodeMetadata{
					Namespace: svcNS,
					Labels:    tc.proxyLabels,
				})
			resp := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
				TypeUrl:       v3.EndpointType,
				ResourceNames: []string{xds.LocalClusterName},
			})

			if len(resp.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
			}
			cla := &endpoint.ClusterLoadAssignment{}
			if err := resp.Resources[0].UnmarshalTo(cla); err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, lle := range cla.Endpoints {
				for _, lbe := range lle.LbEndpoints {
					got[lbe.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()] = true
				}
			}
			if !reflect.DeepEqual(tc.wantAddrs, got) {
				t.Errorf("CLA addresses = %v, want %v", got, tc.wantAddrs)
			}
		})
	}
}

// TestEdsLocalClusterNoService verifies that a proxy with no ServiceTargets that
// subscribes to local_cluster receives an empty CLA (cluster name set, no endpoints)
// rather than a missing resource or a panic.
func TestEdsLocalClusterNoService(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	// Note: no service or instance registered for the proxy's IP, so the proxy will
	// have no ServiceTargets and local_cluster has no underlying service to resolve.
	s.EnsureSynced(t)

	ads := s.ConnectADS().
		WithID("sidecar~9.9.9.9~test.default~default.svc.cluster.local").
		WithType(v3.EndpointType)
	resp := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{xds.LocalClusterName},
	})

	if len(resp.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
	}
	cla := &endpoint.ClusterLoadAssignment{}
	if err := resp.Resources[0].UnmarshalTo(cla); err != nil {
		t.Fatal(err)
	}
	if cla.ClusterName != xds.LocalClusterName {
		t.Errorf("cluster name = %q, want %q", cla.ClusterName, xds.LocalClusterName)
	}
	if len(cla.Endpoints) != 0 {
		t.Errorf("expected empty endpoints, got %d locality groups", len(cla.Endpoints))
	}
}

// TestEdsLocalClusterSteadyStateNoService verifies that for a proxy that never has
// a local service, subsequent unrelated service updates do NOT trigger a new
// local_cluster push. The initial empty CLA is delivered once, and after that the
// PrevLocalService==LocalService==\"\" sync inside buildEndpoints should keep the
// transition flag clear so the partial-push optimization kicks in.
func TestEdsLocalClusterSteadyStateNoService(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	s.EnsureSynced(t)

	ads := s.ConnectADS().
		WithID("sidecar~9.9.9.9~test.default~default.svc.cluster.local").
		WithType(v3.EndpointType).
		WithMetadata(model.NodeMetadata{Namespace: "default"})
	// Consume the initial empty-CLA response so the connection is in steady state.
	_ = ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{xds.LocalClusterName},
	})

	// An unrelated ServiceEntry update in the proxy's namespace must not produce
	// a follow-up local_cluster push.
	s.Discovery.Push(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      "other.default.svc.cluster.local",
			Namespace: "default",
		}),
	})
	ads.ExpectNoResponse(t)
}

// TestEdsLocalClusterServiceReplacement verifies that when the proxy's local service
// is swapped for a service with a different hostname, the transition is detected
// (LocalService != PrevLocalService) and local_cluster is recomputed from the new
// service's endpoints — even on a partial push.
func TestEdsLocalClusterServiceReplacement(t *testing.T) {
	const (
		proxyIP    = "1.1.1.1"
		svcNS      = "default"
		nodeID     = "sidecar~1.1.1.1~test.default~default.svc.cluster.local"
		portName   = "http"
		portNumber = 80
		hostA      = "a.default.svc.cluster.local"
		hostB      = "b.default.svc.cluster.local"
	)

	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})

	mkSvc := func(hostname, name string) *model.Service {
		return &model.Service{
			Hostname: host.Name(hostname),
			Ports: model.PortList{{
				Name:     portName,
				Port:     portNumber,
				Protocol: protocol.HTTP,
			}},
			Attributes: model.ServiceAttributes{Namespace: svcNS, Name: name},
		}
	}
	mkInstance := func(svc *model.Service, zone string) *model.ServiceInstance {
		return &model.ServiceInstance{
			Service:     svc,
			ServicePort: svc.Ports[0],
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{proxyIP},
				ServicePortName: portName,
				EndpointPort:    portNumber,
				Locality:        model.Locality{Label: zone},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
		}
	}

	svcA := mkSvc(hostA, "a")
	s.MemRegistry.AddService(svcA)
	s.MemRegistry.AddInstance(mkInstance(svcA, "region1/zoneA/sub"))
	s.EnsureSynced(t)

	ads := s.ConnectADS().
		WithID(nodeID).
		WithType(v3.EndpointType).
		WithMetadata(model.NodeMetadata{
			Namespace: svcNS,
			Labels: map[string]string{
				"topology.kubernetes.io/region": "region1",
				"topology.kubernetes.io/zone":   "zoneA",
			},
		}).
		WithTimeout(200 * time.Millisecond)
	// Initial state: proxy belongs to svcA. The initial CLA should carry zoneA.
	initial := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{xds.LocalClusterName},
	})
	{
		cla := &endpoint.ClusterLoadAssignment{}
		if err := initial.Resources[0].UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		zones := map[string]bool{}
		for _, lle := range cla.Endpoints {
			zones[lle.Locality.GetZone()] = true
		}
		if !zones["zoneA"] {
			t.Fatalf("initial CLA should contain zoneA, got %v", zones)
		}
	}

	// Swap: remove svcA and add svcB (different hostname) where the proxy's IP is
	// also an endpoint. After SetServiceTargets, proxy.LocalService becomes hostB
	// while PrevLocalService is still hostA, so the transition forces a recompute.
	s.MemRegistry.RemoveService(host.Name(hostA))
	svcB := mkSvc(hostB, "b")
	s.MemRegistry.AddService(svcB)
	s.MemRegistry.AddInstance(mkInstance(svcB, "region1/zoneB/sub"))

	// Read responses until we see the CLA reflect svcB's endpoints. Multiple
	// debounced pushes may arrive (one for the removal, one or more for the add).
	retry.UntilSuccessOrFail(t, func() error {
		return test.Wrap(func(w test.Failer) {
			resp := ads.ExpectResponse(w)
			cla := &endpoint.ClusterLoadAssignment{}
			if err := resp.Resources[0].UnmarshalTo(cla); err != nil {
				w.Fatal(err)
			}
			zones := map[string]bool{}
			for _, lle := range cla.Endpoints {
				zones[lle.Locality.GetZone()] = true
			}
			if !zones["zoneB"] || zones["zoneA"] {
				w.Fatalf("CLA zones = %v, want zoneB only", zones)
			}
		})
	}, retry.Timeout(3*time.Second), retry.Delay(10*time.Millisecond))
}

// TestEdsLocalClusterServiceLifecycle verifies that local_cluster reacts to the
// proxy's local service appearing and disappearing at runtime:
//   - Initial response with no service is an empty CLA.
//   - When the service and a matching endpoint are added, local_cluster is pushed
//     with the new endpoint.
//   - When the service is removed, local_cluster is pushed again as an empty CLA.
func TestEdsLocalClusterServiceLifecycle(t *testing.T) {
	const (
		proxyIP    = "1.1.1.1"
		svcNS      = "default"
		svcName    = "local"
		svcHost    = "local.default.svc.cluster.local"
		nodeID     = "sidecar~1.1.1.1~test.default~default.svc.cluster.local"
		portName   = "http"
		portNumber = 80
	)

	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	s.EnsureSynced(t)

	ads := s.ConnectADS().
		WithID(nodeID).
		WithType(v3.EndpointType).
		WithMetadata(model.NodeMetadata{
			Namespace: svcNS,
			Labels: map[string]string{
				"topology.kubernetes.io/region": "region1",
				"topology.kubernetes.io/zone":   "zone1",
			},
		}).
		WithTimeout(200 * time.Millisecond)
	initial := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{xds.LocalClusterName},
	})

	extractAddrs := func(t *testing.T, resp *discovery.DiscoveryResponse) map[string]bool {
		t.Helper()
		if len(resp.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
		}
		cla := &endpoint.ClusterLoadAssignment{}
		if err := resp.Resources[0].UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		if cla.ClusterName != xds.LocalClusterName {
			t.Errorf("cluster name = %q, want %q", cla.ClusterName, xds.LocalClusterName)
		}
		got := map[string]bool{}
		for _, lle := range cla.Endpoints {
			for _, lbe := range lle.LbEndpoints {
				got[lbe.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()] = true
			}
		}
		return got
	}

	assertLocalCLA := func(t *testing.T, resp *discovery.DiscoveryResponse, wantAddrs ...string) {
		t.Helper()
		got := extractAddrs(t, resp)
		if len(got) != len(wantAddrs) {
			t.Fatalf("CLA endpoints = %v, want %v", got, wantAddrs)
		}
		for _, a := range wantAddrs {
			if !got[a] {
				t.Errorf("expected address %q in CLA, got %v", a, got)
			}
		}
	}

	// waitForCLAState consumes EDS responses until one matches the expected address
	// set (or fails after a longer outer deadline). A registry mutation like
	// AddService+AddInstance often produces multiple debounced pushes; we want to
	// validate the *final* state, not the first response we happen to grab.
	waitForCLAState := func(t *testing.T, wantAddrs ...string) {
		t.Helper()
		want := map[string]bool{}
		for _, a := range wantAddrs {
			want[a] = true
		}
		retry.UntilSuccessOrFail(t, func() error {
			return test.Wrap(func(w test.Failer) {
				resp := ads.ExpectResponse(w)
				got := extractAddrs(t, resp)
				if !reflect.DeepEqual(got, want) {
					w.Fatalf("CLA endpoints = %v, want %v", got, want)
				}
			})
		}, retry.Timeout(3*time.Second), retry.Delay(10*time.Millisecond))
	}

	t.Run("no service yields empty CLA", func(t *testing.T) {
		assertLocalCLA(t, initial)
	})

	svc := &model.Service{
		Hostname: host.Name(svcHost),
		Ports: model.PortList{{
			Name:     portName,
			Port:     portNumber,
			Protocol: protocol.HTTP,
		}},
		Attributes: model.ServiceAttributes{Namespace: svcNS, Name: svcName},
	}

	t.Run("adding service populates CLA", func(t *testing.T) {
		s.MemRegistry.AddService(svc)
		s.MemRegistry.AddInstance(&model.ServiceInstance{
			Service:     svc,
			ServicePort: svc.Ports[0],
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{proxyIP},
				ServicePortName: portName,
				EndpointPort:    portNumber,
				Locality:        model.Locality{Label: "region1/zone1/subzone1"},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
		})
		waitForCLAState(t, proxyIP)
	})

	t.Run("removing service yields empty CLA", func(t *testing.T) {
		s.MemRegistry.RemoveService(host.Name(svcHost))
		waitForCLAState(t)
	})
}

// TestEdsZoneAwareDestinationRule verifies the end-to-end EDS flow for a service that
// has a DestinationRule with ZoneAwareLbSetting + outlier detection. The resulting CLA
// must be region-bucketed: every same-region locality at priority 0 (regardless of
// zone/subzone), failover-matched region at priority 1, remaining regions compacted
// past that.
func TestEdsZoneAwareDestinationRule(t *testing.T) {
	const (
		svcHost      = "zoneaware.cluster.local"
		clusterName  = "outbound|80||zoneaware.cluster.local"
		proxyAddress = "10.10.10.10"
	)
	drYAML := `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: zone-aware
  namespace: default
spec:
  host: zoneaware.cluster.local
  trafficPolicy:
    outlierDetection:
      interval: 1s
      baseEjectionTime: 3m
      maxEjectionPercent: 100
    loadBalancer:
      zoneAwareLbSetting:
        enabled: true
        failover:
        - from: region1
          to: region2
`
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{ConfigString: drYAML})

	svc := &model.Service{
		Hostname:       host.Name(svcHost),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		}},
		Attributes: model.ServiceAttributes{Namespace: "default", Name: "zoneaware"},
	}
	s.MemRegistry.AddService(svc)

	// Endpoints span three regions; within region1 we add two zones to confirm both
	// land at priority 0 even though only one matches the proxy's full locality.
	endpoints := []struct {
		ip       string
		locality string
	}{
		{"10.0.1.1", "region1/zone1/subzone1"},
		{"10.0.1.2", "region1/zone2/subzone1"},
		{"10.0.2.1", "region2/zone1/subzone1"},
		{"10.0.3.1", "region3/zone1/subzone1"},
	}
	for _, e := range endpoints {
		s.MemRegistry.AddInstance(&model.ServiceInstance{
			Service:     svc,
			ServicePort: svc.Ports[0],
			Endpoint: &model.IstioEndpoint{
				Addresses:       []string{e.ip},
				ServicePortName: "http",
				EndpointPort:    80,
				Locality:        model.Locality{Label: e.locality},
				TLSMode:         model.IstioMutualTLSModeLabel,
				HealthStatus:    model.Healthy,
			},
		})
	}
	s.EnsureSynced(t)

	adscConn := s.Connect(
		&model.Proxy{Locality: util.ConvertLocality(asdcLocality), IPAddresses: []string{proxyAddress}},
		nil, watchAll,
	)
	cla := adscConn.GetEndpoints()[clusterName]
	if cla == nil {
		clusters := make([]string, 0, len(adscConn.GetEndpoints()))
		for k := range adscConn.GetEndpoints() {
			clusters = append(clusters, k)
		}
		t.Fatalf("no CLA for %s; got clusters %v", clusterName, clusters)
	}
	// Map each endpoint IP to its priority for stable assertions.
	gotPriority := map[string]uint32{}
	for _, lle := range cla.GetEndpoints() {
		for _, lbe := range lle.LbEndpoints {
			gotPriority[lbe.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()] = lle.Priority
		}
	}
	want := map[string]uint32{
		"10.0.1.1": 0, // region1/zone1/subzone1 (proxy locality)
		"10.0.1.2": 0, // region1, different zone — still p0 under zone-aware
		"10.0.2.1": 1, // region2 - failover To
		"10.0.3.1": 2, // region3 - failover unmatched
	}
	if !reflect.DeepEqual(want, gotPriority) {
		t.Errorf("CLA priorities = %v, want %v", gotPriority, want)
	}
}
