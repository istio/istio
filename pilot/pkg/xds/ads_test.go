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
	"fmt"
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	xdsfake "istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tests/util/leak"
)

const (
	testConfigNamespace = "default"

	routeA = "http.80"
	routeB = "https.443.https.my-gateway.testns"
)

func TestAdsReconnectAfterRestart(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})

	ads := s.ConnectADS().WithType(v3.EndpointType)
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{"fake-cluster"}})
	// Close the connection and reconnect
	ads.Cleanup()

	ads = s.ConnectADS().WithType(v3.EndpointType)

	// Reconnect with the same resources
	ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		ResourceNames: []string{"fake-cluster"},
		ResponseNonce: res.Nonce,
		VersionInfo:   res.VersionInfo,
	})
}

// TestAdsReconnectRequests provides a regression test for a case where Envoy sends an EDS request as the first
// request on a connection.
func TestAdsReconnectRequests(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})

	ads := s.ConnectADS()
	// Send normal CDS and EDS requests
	_ = ads.RequestResponseAck(t, &discovery.DiscoveryRequest{TypeUrl: v3.ClusterType})
	eres := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{TypeUrl: v3.EndpointType, ResourceNames: []string{"my-resource"}})

	// A push should get a response for both
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, Forced: true})
	ads.ExpectResponse(t)
	ads.ExpectResponse(t)
	// Close the connection and reconnect
	ads.Cleanup()
	ads = s.ConnectADS()

	// Send a request for EDS version 1 - we do not explicitly ACK this.
	ads.Request(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{"my-resource"},
		ResponseNonce: eres.Nonce,
	})
	// We should get a response
	eres3 := ads.ExpectResponse(t)
	// Now send our CDS request
	ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.ClusterType,
		ResponseNonce: eres.Nonce,
	})
	// Send another request. This is essentially an ACK of eres3. However, envoy expects a response
	ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{"my-resource"},
		ResponseNonce: eres3.Nonce,
	})
}

func TestAdsUnsubscribe(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})

	ads := s.ConnectADS().WithType(v3.EndpointType)
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{"fake-cluster"}})

	ads.Request(t, &discovery.DiscoveryRequest{
		ResourceNames: nil,
		ResponseNonce: res.Nonce,
		VersionInfo:   res.VersionInfo,
	})
	ads.ExpectNoResponse(t)
}

// Regression for envoy restart and overlapping connections
func TestAdsReconnect(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.ClusterType)
	ads.RequestResponseAck(t, nil)

	// envoy restarts and reconnects
	ads2 := s.ConnectADS().WithType(v3.ClusterType)
	ads2.RequestResponseAck(t, nil)

	// closes old process
	ads.Cleanup()

	// event happens, expect push to the remaining connection
	xds.AdsPushAll(s.Discovery)
	ads2.ExpectResponse(t)
}

// Regression for connection with a bad ID
func TestAdsBadId(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS().WithID("").WithType(v3.ClusterType)
	xds.AdsPushAll(s.Discovery)
	ads.ExpectNoResponse(t)
}

func TestVersionNonce(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.ClusterType)
	resp1 := ads.RequestResponseAck(t, nil)
	fullPush(s)
	resp2 := ads.ExpectResponse(t)
	if !(resp1.VersionInfo < resp2.VersionInfo) {
		t.Fatalf("version should be incrementing: %v -> %v", resp1.VersionInfo, resp2.VersionInfo)
	}
	if resp1.Nonce == resp2.Nonce {
		t.Fatalf("nonce should change %v -> %v", resp1.Nonce, resp2.Nonce)
	}
}

func TestAdsClusterUpdate(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.EndpointType)

	version := ""
	nonce := ""
	sendEDSReqAndVerify := func(clusterName string) {
		res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
			ResourceNames: []string{clusterName},
			VersionInfo:   version,
			ResponseNonce: nonce,
		})
		version = res.VersionInfo
		nonce = res.Nonce
		got := xdstest.MapKeys(xdstest.ExtractLoadAssignments(xdstest.UnmarshalClusterLoadAssignment(t, res.Resources)))
		if len(got) != 1 {
			t.Fatalf("expected 1 response, got %v", len(got))
		}
		if got[0] != clusterName {
			t.Fatalf("expected cluster %v got %v", clusterName, got[0])
		}
	}

	cluster1 := "outbound|80||local.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster1)
	cluster2 := "outbound|80||hello.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster2)
}

// nolint: lll
func TestAdsPushScoping(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})

	const (
		svcSuffix = ".testPushScoping.com"
		ns1       = "ns1"
	)

	removeServiceByNames := func(ns string, names ...string) {
		configsUpdated := sets.New[model.ConfigKey]()

		for _, name := range names {
			hostname := host.Name(name)
			s.MemRegistry.RemoveService(hostname)
			configsUpdated.Insert(model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      string(hostname),
				Namespace: ns,
			})
		}

		s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, ConfigsUpdated: configsUpdated})
	}
	removeService := func(ns string, indexes ...int) {
		var names []string

		for _, i := range indexes {
			names = append(names, fmt.Sprintf("svc%d%s", i, svcSuffix))
		}

		removeServiceByNames(ns, names...)
	}
	addServiceByNames := func(ns string, names ...string) {
		configsUpdated := sets.New[model.ConfigKey]()

		for _, name := range names {
			hostname := host.Name(name)
			configsUpdated.Insert(model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      string(hostname),
				Namespace: ns,
			})

			s.MemRegistry.AddService(&model.Service{
				Hostname:       hostname,
				DefaultAddress: "10.11.0.1",
				Ports: []*model.Port{
					{
						Name:     "http-main",
						Port:     2080,
						Protocol: protocol.HTTP,
					},
				},
				Attributes: model.ServiceAttributes{
					Namespace: ns,
				},
			})
		}

		s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, ConfigsUpdated: configsUpdated})
	}
	addService := func(ns string, indexes ...int) {
		var hostnames []string
		for _, i := range indexes {
			hostnames = append(hostnames, fmt.Sprintf("svc%d%s", i, svcSuffix))
		}
		addServiceByNames(ns, hostnames...)
	}

	addServiceInstance := func(hostname host.Name, indexes ...int) {
		for _, i := range indexes {
			s.MemRegistry.AddEndpoint(hostname, "http-main", 2080, "192.168.1.10", i)
		}

		s.Discovery.ConfigUpdate(&model.PushRequest{Full: false, ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: string(hostname), Namespace: testConfigNamespace})})
	}

	addVirtualService := func(i int, hosts []string, dest string) {
		if _, err := s.Store().Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             fmt.Sprintf("vs%d", i), Namespace: testConfigNamespace,
			},
			Spec: &networking.VirtualService{
				Hosts: hosts,
				Http: []*networking.HTTPRoute{{
					Name: "dest-foo",
					Route: []*networking.HTTPRouteDestination{{
						Destination: &networking.Destination{
							Host: dest,
						},
					}},
				}},
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	removeVirtualService := func(i int) {
		s.Store().Delete(gvk.VirtualService, fmt.Sprintf("vs%d", i), testConfigNamespace, nil)
	}

	addDelegateVirtualService := func(i int, hosts []string, dest string) {
		if _, err := s.Store().Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             fmt.Sprintf("rootvs%d", i), Namespace: testConfigNamespace,
			},
			Spec: &networking.VirtualService{
				Hosts: hosts,

				Http: []*networking.HTTPRoute{{
					Name: "dest-foo",
					Delegate: &networking.Delegate{
						Name:      fmt.Sprintf("delegatevs%d", i),
						Namespace: testConfigNamespace,
					},
				}},
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := s.Store().Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             fmt.Sprintf("delegatevs%d", i), Namespace: testConfigNamespace,
			},
			Spec: &networking.VirtualService{
				Http: []*networking.HTTPRoute{{
					Name: "dest-foo",
					Route: []*networking.HTTPRouteDestination{{
						Destination: &networking.Destination{
							Host: dest,
						},
					}},
				}},
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	updateDelegateVirtualService := func(i int, dest string) {
		if _, err := s.Store().Update(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             fmt.Sprintf("delegatevs%d", i), Namespace: testConfigNamespace,
			},
			Spec: &networking.VirtualService{
				Http: []*networking.HTTPRoute{{
					Name: "dest-foo",
					Headers: &networking.Headers{
						Request: &networking.Headers_HeaderOperations{
							Remove: []string{"any-string"},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: dest,
							},
						},
					},
				}},
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	removeDelegateVirtualService := func(i int) {
		s.Store().Delete(gvk.VirtualService, fmt.Sprintf("rootvs%d", i), testConfigNamespace, nil)
		s.Store().Delete(gvk.VirtualService, fmt.Sprintf("delegatevs%d", i), testConfigNamespace, nil)
	}

	addDestinationRule := func(i int, host string) {
		if _, err := s.Store().Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
				Name:             fmt.Sprintf("dr%d", i), Namespace: testConfigNamespace,
			},
			Spec: &networking.DestinationRule{
				Host:     host,
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	removeDestinationRule := func(i int) {
		s.Store().Delete(gvk.DestinationRule, fmt.Sprintf("dr%d", i), testConfigNamespace, nil)
	}

	sc := &networking.Sidecar{
		Egress: []*networking.IstioEgressListener{
			{
				Hosts: []string{testConfigNamespace + "/*" + svcSuffix},
			},
		},
	}
	scc := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Sidecar,
			Name:             "sc", Namespace: testConfigNamespace,
		},
		Spec: sc,
	}
	notMatchedScc := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Sidecar,
			Name:             "notMatchedSc", Namespace: testConfigNamespace,
		},
		Spec: &networking.Sidecar{
			WorkloadSelector: &networking.WorkloadSelector{
				Labels: map[string]string{"notMatched": "notMatched"},
			},
		},
	}
	if _, err := s.Store().Create(scc); err != nil {
		t.Fatal(err)
	}
	addService(testConfigNamespace, 1, 2, 3)

	adscConn := s.Connect(nil, nil, nil)
	defer adscConn.Close()
	type svcCase struct {
		desc string

		ev          model.Event
		svcIndexes  []int
		svcNames    []string
		ns          string
		instIndexes []struct {
			name    string
			indexes []int
		}
		vsIndexes []struct {
			index int
			hosts []string
			dest  string
		}
		delegatevsIndexes []struct {
			index int
			hosts []string
			dest  string
		}
		drIndexes []struct {
			index int
			host  string
		}
		cfgs []config.Config

		expectedUpdates   []string
		unexpectedUpdates []string
	}
	svcCases := []svcCase{
		{
			desc:            "Add a scoped service",
			ev:              model.EventAdd,
			svcIndexes:      []int{4},
			ns:              testConfigNamespace,
			expectedUpdates: []string{v3.ListenerType},
		}, // then: default 1,2,3,4
		{
			desc: "Add instances to a scoped service",
			ev:   model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{fmt.Sprintf("svc%d%s", 4, svcSuffix), []int{1, 2}}},
			ns:              testConfigNamespace,
			expectedUpdates: []string{v3.EndpointType},
		}, // then: default 1,2,3,4
		{
			desc: "Add virtual service to a scoped service",
			ev:   model.EventAdd,
			vsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4, hosts: []string{fmt.Sprintf("svc%d%s", 4, svcSuffix)}, dest: "unknown-svc"}},
			expectedUpdates: []string{v3.ListenerType},
		},
		{
			desc: "Delete virtual service of a scoped service",
			ev:   model.EventDelete,
			vsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4}},
			expectedUpdates: []string{v3.ListenerType},
		},
		{
			desc: "Add destination rule to a scoped service",
			ev:   model.EventAdd,
			drIndexes: []struct {
				index int
				host  string
			}{{4, fmt.Sprintf("svc%d%s", 4, svcSuffix)}},
			expectedUpdates: []string{v3.ClusterType},
		},
		{
			desc: "Delete destination rule of a scoped service",
			ev:   model.EventDelete,
			drIndexes: []struct {
				index int
				host  string
			}{{index: 4}},
			expectedUpdates: []string{v3.ClusterType},
		},
		{
			desc:              "Add a unscoped(name not match) service",
			ev:                model.EventAdd,
			svcNames:          []string{"foo.com"},
			ns:                testConfigNamespace,
			unexpectedUpdates: []string{v3.ClusterType},
		}, // then: default 1,2,3,4, foo.com; ns1: 11
		{
			desc: "Add instances to an unscoped service",
			ev:   model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{"foo.com", []int{1, 2}}},
			ns:                testConfigNamespace,
			unexpectedUpdates: []string{v3.EndpointType},
		}, // then: default 1,2,3,4
		{
			desc:              "Add a unscoped(ns not match) service",
			ev:                model.EventAdd,
			svcIndexes:        []int{11},
			ns:                ns1,
			unexpectedUpdates: []string{v3.ClusterType},
		}, // then: default 1,2,3,4, foo.com; ns1: 11
		{
			desc: "Add virtual service to an unscoped service",
			ev:   model.EventAdd,
			vsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 0, hosts: []string{"foo.com"}, dest: "unknown-service"}},
			unexpectedUpdates: []string{v3.ClusterType},
		},
		{
			desc: "Delete virtual service of a unscoped service",
			ev:   model.EventDelete,
			vsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 0}},
			unexpectedUpdates: []string{v3.ClusterType},
		},
		{
			desc: "Add destination rule to an unscoped service",
			ev:   model.EventAdd,
			drIndexes: []struct {
				index int
				host  string
			}{{0, "foo.com"}},
			unexpectedUpdates: []string{v3.ClusterType},
		},
		{
			desc: "Delete destination rule of a unscoped service",
			ev:   model.EventDelete,
			drIndexes: []struct {
				index int
				host  string
			}{{index: 0}},
			unexpectedUpdates: []string{v3.ClusterType},
		},
		{
			desc: "Add virtual service for scoped service with transitively scoped dest svc",
			ev:   model.EventAdd,
			vsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4, hosts: []string{fmt.Sprintf("svc%d%s", 4, svcSuffix)}, dest: "foo.com"}},
			expectedUpdates: []string{v3.ClusterType, v3.EndpointType},
		},
		{
			desc: "Add instances for transitively scoped svc",
			ev:   model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{"foo.com", []int{1, 2}}},
			ns:              testConfigNamespace,
			expectedUpdates: []string{v3.EndpointType},
		},
		{
			desc: "Delete virtual service for scoped service with transitively scoped dest svc",
			ev:   model.EventDelete,
			vsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4}},
			expectedUpdates: []string{v3.ClusterType},
		},
		{
			desc: "Add delegation virtual service for scoped service with transitively scoped dest svc",
			ev:   model.EventAdd,
			delegatevsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4, hosts: []string{fmt.Sprintf("svc%d%s", 4, svcSuffix)}, dest: "foo.com"}},
			expectedUpdates: []string{v3.ListenerType, v3.RouteType, v3.ClusterType, v3.EndpointType},
		},
		{
			desc: "Update delegate virtual service should trigger full push",
			ev:   model.EventUpdate,
			delegatevsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4, hosts: []string{fmt.Sprintf("svc%d%s", 4, svcSuffix)}, dest: "foo.com"}},
			expectedUpdates: []string{v3.ListenerType, v3.RouteType, v3.ClusterType},
		},
		{
			desc: "Delete delegate virtual service for scoped service with transitively scoped dest svc",
			ev:   model.EventDelete,
			delegatevsIndexes: []struct {
				index int
				hosts []string
				dest  string
			}{{index: 4}},
			expectedUpdates: []string{v3.ListenerType, v3.RouteType, v3.ClusterType},
		},
		{
			desc:            "Remove a scoped service",
			ev:              model.EventDelete,
			svcIndexes:      []int{4},
			ns:              testConfigNamespace,
			expectedUpdates: []string{v3.ListenerType},
		}, // then: default 1,2,3, foo.com; ns: 11
		{
			desc:              "Remove a unscoped(name not match) service",
			ev:                model.EventDelete,
			svcNames:          []string{"foo.com"},
			ns:                testConfigNamespace,
			unexpectedUpdates: []string{v3.ClusterType},
		}, // then: default 1,2,3; ns1: 11
		{
			desc:              "Remove a unscoped(ns not match) service",
			ev:                model.EventDelete,
			svcIndexes:        []int{11},
			ns:                ns1,
			unexpectedUpdates: []string{v3.ClusterType},
		}, // then: default 1,2,3
		{
			desc:              "Add an unmatched Sidecar config",
			ev:                model.EventAdd,
			cfgs:              []config.Config{notMatchedScc},
			ns:                testConfigNamespace,
			unexpectedUpdates: []string{v3.ListenerType, v3.RouteType, v3.ClusterType, v3.EndpointType},
		},
		{
			desc:            "Update the Sidecar config",
			ev:              model.EventUpdate,
			cfgs:            []config.Config{scc},
			ns:              testConfigNamespace,
			expectedUpdates: []string{v3.ListenerType, v3.RouteType, v3.ClusterType, v3.EndpointType},
		},
	}

	for _, c := range svcCases {
		t.Run(c.desc, func(t *testing.T) {
			// Let events from previous tests complete
			time.Sleep(time.Millisecond * 50)
			adscConn.WaitClear()
			var wantUpdates []string
			wantUpdates = append(wantUpdates, c.expectedUpdates...)
			wantUpdates = append(wantUpdates, c.unexpectedUpdates...)

			switch c.ev {
			case model.EventAdd:
				if len(c.svcIndexes) > 0 {
					addService(c.ns, c.svcIndexes...)
				}
				if len(c.svcNames) > 0 {
					addServiceByNames(c.ns, c.svcNames...)
				}
				if len(c.instIndexes) > 0 {
					for _, instIndex := range c.instIndexes {
						addServiceInstance(host.Name(instIndex.name), instIndex.indexes...)
					}
				}
				if len(c.vsIndexes) > 0 {
					for _, vsIndex := range c.vsIndexes {
						addVirtualService(vsIndex.index, vsIndex.hosts, vsIndex.dest)
					}
				}
				if len(c.delegatevsIndexes) > 0 {
					for _, vsIndex := range c.delegatevsIndexes {
						addDelegateVirtualService(vsIndex.index, vsIndex.hosts, vsIndex.dest)
					}
				}
				if len(c.drIndexes) > 0 {
					for _, drIndex := range c.drIndexes {
						addDestinationRule(drIndex.index, drIndex.host)
					}
				}
				if len(c.cfgs) > 0 {
					for _, cfg := range c.cfgs {
						if _, err := s.Store().Create(cfg); err != nil {
							t.Fatal(err)
						}
					}
				}
			case model.EventUpdate:
				if len(c.delegatevsIndexes) > 0 {
					for _, vsIndex := range c.delegatevsIndexes {
						updateDelegateVirtualService(vsIndex.index, vsIndex.dest)
					}
				}
				if len(c.cfgs) > 0 {
					for _, cfg := range c.cfgs {
						if _, err := s.Store().Update(cfg); err != nil {
							t.Fatal(err)
						}
					}
				}
			case model.EventDelete:
				if len(c.svcIndexes) > 0 {
					removeService(c.ns, c.svcIndexes...)
				}
				if len(c.svcNames) > 0 {
					removeServiceByNames(c.ns, c.svcNames...)
				}
				if len(c.vsIndexes) > 0 {
					for _, vsIndex := range c.vsIndexes {
						removeVirtualService(vsIndex.index)
					}
				}
				if len(c.delegatevsIndexes) > 0 {
					for _, vsIndex := range c.delegatevsIndexes {
						removeDelegateVirtualService(vsIndex.index)
					}
				}
				if len(c.drIndexes) > 0 {
					for _, drIndex := range c.drIndexes {
						removeDestinationRule(drIndex.index)
					}
				}
			default:
				t.Fatalf("wrong event for case %v", c)
			}

			timeout := time.Millisecond * 200
			upd, _ := adscConn.Wait(timeout, wantUpdates...)
			for _, expect := range c.expectedUpdates {
				if !slices.Contains(upd, expect) {
					t.Fatalf("expected update %s not in updates %v", expect, upd)
				}
			}
			for _, unexpect := range c.unexpectedUpdates {
				if slices.Contains(upd, unexpect) {
					t.Fatalf("expected to not get update %s, but it is in updates %v", unexpect, upd)
				}
			}
		})
	}
}

func TestAdsUpdate(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS()

	s.MemRegistry.AddService(&model.Service{
		Hostname:       "adsupdate.default.svc.cluster.local",
		DefaultAddress: "10.11.0.1",
		Ports: []*model.Port{
			{
				Name:     "http-main",
				Port:     2080,
				Protocol: protocol.HTTP,
			},
		},
		Attributes: model.ServiceAttributes{
			Name:      "adsupdate",
			Namespace: "default",
		},
	})
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, Forced: true})
	time.Sleep(time.Millisecond * 200)
	s.MemRegistry.SetEndpoints("adsupdate.default.svc.cluster.local", "default",
		newEndpointWithAccount("10.2.0.1", "hello-sa", "v1"))

	cluster := "outbound|2080||adsupdate.default.svc.cluster.local"
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		ResourceNames: []string{cluster},
		TypeUrl:       v3.EndpointType,
	})
	eps, f := xdstest.ExtractLoadAssignments(xdstest.UnmarshalClusterLoadAssignment(t, res.GetResources()))[cluster]
	if !f {
		t.Fatalf("did not find cluster %v", cluster)
	}
	if !reflect.DeepEqual(eps, []string{"10.2.0.1:80"}) {
		t.Fatalf("expected endpoints [10.2.0.1:80] got %v", eps)
	}

	_ = s.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.1.7.1", 1080)

	// will trigger recompute and push for all clients - including some that may be closing
	// This reproduced the 'push on closed connection' bug.
	xds.AdsPushAll(s.Discovery)
	res1 := ads.ExpectResponse(t)
	xdstest.UnmarshalClusterLoadAssignment(t, res1.GetResources())
}

func TestEnvoyRDSProtocolError(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.RouteType)
	ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{routeA}})

	xds.AdsPushAll(s.Discovery)
	res := ads.ExpectResponse(t)

	// send empty response and validate no response is returned.
	ads.Request(t, &discovery.DiscoveryRequest{
		ResourceNames: nil,
		VersionInfo:   res.VersionInfo,
		ResponseNonce: res.Nonce,
	})
	ads.ExpectNoResponse(t)

	// Refresh routes
	ads.Request(t, &discovery.DiscoveryRequest{
		ResourceNames: []string{routeA, routeB},
		VersionInfo:   res.VersionInfo,
		ResponseNonce: res.Nonce,
	})
}

func TestEnvoyRDSUpdatedRouteRequest(t *testing.T) {
	expectRoutes := func(resp *discovery.DiscoveryResponse, expected ...string) {
		t.Helper()
		got := xdstest.MapKeys(xdstest.ExtractRouteConfigurations(xdstest.UnmarshalRouteConfiguration(t, resp.Resources)))
		if !reflect.DeepEqual(expected, got) {
			t.Fatalf("expected routes %v got %v", expected, got)
		}
	}
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.RouteType)
	resp := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{routeA}})
	expectRoutes(resp, routeA)

	xds.AdsPushAll(s.Discovery)
	resp = ads.ExpectResponse(t)
	expectRoutes(resp, routeA)

	// Test update from A -> B
	resp = ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{routeB}})
	expectRoutes(resp, routeB)

	// Test update from B -> A, B
	resp = ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{routeA, routeB}})
	expectRoutes(resp, routeA, routeB)

	// Test update from B, B -> A
	resp = ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{routeA}})
	expectRoutes(resp, routeA)
}

func TestEdsCache(t *testing.T) {
	makeEndpoint := func(addr []*networking.WorkloadEntry) config.Config {
		return config.Config{
			Meta: config.Meta{
				Name:             "service",
				Namespace:        "default",
				GroupVersionKind: gvk.ServiceEntry,
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"foo.com"},
				Ports: []*networking.ServicePort{{
					Number:   80,
					Protocol: "HTTP",
					Name:     "http",
				}},
				Resolution: networking.ServiceEntry_STATIC,
				Endpoints:  addr,
			},
		}
	}
	assertEndpoints := func(a *adsc.ADSC, addr ...string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			got := sets.New(xdstest.ExtractEndpoints(a.GetEndpoints()["outbound|80||foo.com"])...)
			want := sets.New(addr...)

			if !got.Equals(want) {
				return fmt.Errorf("invalid endpoints, got %v want %v", got, addr)
			}
			return nil
		}, retry.Timeout(time.Second*5))
	}

	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{
		Configs: []config.Config{
			makeEndpoint([]*networking.WorkloadEntry{
				{Address: "1.2.3.4", Locality: "region/zone"},
				{Address: "1.2.3.5", Locality: "notmatch"},
			}),
		},
	})
	ads := s.Connect(&model.Proxy{Locality: &core.Locality{Region: "region"}}, nil, watchAll)

	assertEndpoints(ads, "1.2.3.4:80", "1.2.3.5:80")
	t.Logf("endpoints: %+v", xdstest.ExtractEndpoints(ads.GetEndpoints()["outbound|80||foo.com"]))

	if _, err := s.Store().Update(makeEndpoint([]*networking.WorkloadEntry{
		{Address: "1.2.3.6", Locality: "region/zone"},
		{Address: "1.2.3.5", Locality: "notmatch"},
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := ads.Wait(time.Second*5, v3.EndpointType); err != nil {
		t.Fatal(err)
	}
	assertEndpoints(ads, "1.2.3.6:80", "1.2.3.5:80")
	t.Logf("endpoints: %+v", xdstest.ExtractEndpoints(ads.GetEndpoints()["outbound|80||foo.com"]))

	ads.WaitClear()
	if _, err := s.Store().Create(config.Config{
		Meta: config.Meta{
			Name:             "service",
			Namespace:        "default",
			GroupVersionKind: gvk.DestinationRule,
		},
		Spec: &networking.DestinationRule{
			Host: "foo.com",
			TrafficPolicy: &networking.TrafficPolicy{
				OutlierDetection: &networking.OutlierDetection{},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ads.Wait(time.Second*5, v3.EndpointType); err != nil {
		t.Fatal(err)
	}
	assertEndpoints(ads, "1.2.3.6:80", "1.2.3.5:80")
	retry.UntilSuccessOrFail(t, func() error {
		found := false
		for _, ep := range ads.GetEndpoints()["outbound|80||foo.com"].Endpoints {
			if ep.Priority == 1 {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("locality did not update")
		}
		return nil
	}, retry.Timeout(time.Second*5))

	ads.WaitClear()

	ep := makeEndpoint([]*networking.WorkloadEntry{{Address: "1.2.3.6", Locality: "region/zone"}, {Address: "1.2.3.5", Locality: "notmatch"}})
	ep.Spec.(*networking.ServiceEntry).Resolution = networking.ServiceEntry_DNS
	if _, err := s.Store().Update(ep); err != nil {
		t.Fatal(err)
	}
	if _, err := ads.Wait(time.Second*5, v3.EndpointType); err != nil {
		t.Fatal(err)
	}
	assertEndpoints(ads)
	t.Logf("endpoints: %+v", ads.GetEndpoints())
}

// TestPushQueueLeak is a regression test for https://github.com/grpc/grpc-go/issues/4758
func TestPushQueueLeak(t *testing.T) {
	ds := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	p := ds.ConnectADS()
	p.RequestResponseAck(t, nil)
	for _, c := range ds.Discovery.AllClients() {
		leak.MustGarbageCollect(t, c)
	}
	ds.Discovery.AdsPushAll(&model.PushRequest{Push: ds.PushContext(), Forced: true})
	p.Cleanup()
}

// TestEnableTrailingDotComputation tests the logic that determines whether to enable
// trailing dots in virtual host domains based on client metadata and auto-detection.
func TestEnableTrailingDotComputation(t *testing.T) {
	ds := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})

	tests := []struct {
		name                 string
		userAgentName        string
		metadata             model.NodeMetadata
		wantEnableTrailingDot bool
	}{
		{
			name:                 "gRPC Java client - auto-detected as disabled",
			userAgentName:        "gRPC Java",
			metadata:             model.NodeMetadata{},
			wantEnableTrailingDot: false,
		},
		{
			name:                 "Envoy client - enabled by default",
			userAgentName:        "envoy",
			metadata:             model.NodeMetadata{},
			wantEnableTrailingDot: true,
		},
		{
			name:                 "gRPC Go client - enabled by default",
			userAgentName:        "gRPC Go",
			metadata:             model.NodeMetadata{},
			wantEnableTrailingDot: true,
		},
		{
			name:          "explicit override - disable with 'false'",
			userAgentName: "envoy",
			metadata: model.NodeMetadata{
				Raw: map[string]any{
					"TRAILING_DOT": "false",
				},
			},
			wantEnableTrailingDot: false,
		},
		{
			name:          "explicit override - enable with 'true' (gRPC Java)",
			userAgentName: "gRPC Java",
			metadata: model.NodeMetadata{
				Raw: map[string]any{
					"TRAILING_DOT": "true",
				},
			},
			wantEnableTrailingDot: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Connect as a client with specific metadata
			ads := ds.ConnectADS().
				WithType(v3.ClusterType).
				WithMetadata(tt.metadata)

			// Update the test node to include the user agent - use sidecar as node type
			ads.ID = "sidecar~1.1.1.1~test-node.default~default.svc.cluster.local"
			
			// Send a request which will trigger initProxyMetadata
			// Build the metadata struct properly by including both typed and raw fields
			metadata := tt.metadata.ToStruct()
			// Add the Raw fields explicitly since ToStruct doesn't include them (they're marked json:"-")
			if tt.metadata.Raw != nil {
				for k, v := range tt.metadata.Raw {
					if metadata.Fields == nil {
						metadata.Fields = make(map[string]*structpb.Value)
					}
					val, _ := structpb.NewValue(v)
					metadata.Fields[k] = val
				}
			}
			
			node := &core.Node{
				Id:            ads.ID,
				UserAgentName: tt.userAgentName,
				Metadata:      metadata,
			}
			ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
				Node: node,
			})

			// Find the proxy that was created
			var foundProxy *model.Proxy
			for _, conn := range ds.Discovery.Clients() {
				proxy := conn.Proxy()
				if proxy != nil && proxy.XdsNode != nil && proxy.XdsNode.UserAgentName == tt.userAgentName {
					foundProxy = proxy
					break
				}
			}

			if foundProxy == nil {
				t.Fatal("Could not find proxy for test")
			}

			if foundProxy.Metadata == nil {
				t.Fatal("proxy.Metadata is nil")
			}

			got := bool(foundProxy.Metadata.EnableTrailingDot)
			if got != tt.wantEnableTrailingDot {
				t.Errorf("EnableTrailingDot = %v, want %v", got, tt.wantEnableTrailingDot)
			}
			
			ads.Cleanup()
		})
	}
}
