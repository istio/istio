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
	"context"
	"fmt"
	"testing"
	"time"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/proto"

	mesh "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	istioagent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/security"
	"istio.io/istio/tests/util"
)

const (
	routeA = "http.80"
	routeB = "https.443.https.my-gateway.testns"
)

type clientSecrets struct {
	security.SecretItem
}

func (sc *clientSecrets) GenerateSecret(ctx context.Context, connectionID, resourceName, token string) (*security.SecretItem, error) {
	return &sc.SecretItem, nil
}

// ShouldWaitForGatewaySecret indicates whether a valid gateway secret is expected.
func (sc *clientSecrets) ShouldWaitForGatewaySecret(connectionID, resourceName, token string, fileMountedCertsOnly bool) bool {
	return false
}

// TODO: must fix SDS, it uses existence to detect it's an ACK !!
func (sc *clientSecrets) SecretExist(connectionID, resourceName, token, version string) bool {
	return false
}

// DeleteSecret deletes a secret by its key from cache.
func (sc *clientSecrets) DeleteSecret(connectionID, resourceName string) {
}

// TestAgent will start istiod with TLS enabled, use the istio-agent to connect, and then
// use the ADSC to connect to the agent proxy.
func TestAgent(t *testing.T) {
	// Start Istiod
	bs, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	// TODO: when authz is implemented, verify labels are checked.
	cert, key, err := bs.CA.GenKeyCert([]string{"spiffe://cluster.local/fake.test"}, 1*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	creds := &clientSecrets{
		security.SecretItem{
			PrivateKey:       key,
			CertificateChain: cert,
			RootCert:         bs.CA.GetCAKeyCertBundle().GetRootCertPem(),
		},
	}

	t.Run("agentProxy", func(t *testing.T) {
		// Start the istio-agent (proxy and SDS part) - will connect to XDS
		sa := istioagent.NewAgent(&mesh.ProxyConfig{
			DiscoveryAddress:       util.MockPilotSGrpcAddr,
			ControlPlaneAuthPolicy: mesh.AuthenticationPolicy_MUTUAL_TLS,
		}, &istioagent.AgentConfig{
			// Enable proxy - off by default, will be XDS_LOCAL env in install.
			LocalXDSAddr: "127.0.0.1:15002",
		}, &security.Options{
			PilotCertProvider: "custom",
			ClusterID:         "kubernetes",
		})

		// Override agent auth - start will use this instead of a gRPC
		// TODO: add a test for cert-based config.
		// TODO: add a test for JWT-based ( using some mock OIDC in Istiod)
		sa.WorkloadSecrets = creds
		_, err = sa.Start(true, "test")
		if err != nil {
			t.Fatal(err)
		}

		// connect to the local XDS proxy - it's using a transient port.
		ldsr, err := adsc.Dial(sa.LocalXDSListener.Addr().String(), "",
			&adsc.Config{
				IP:        "10.11.10.1",
				Namespace: "test",
				Watch: []string{
					v3.ClusterType,
					collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind().String()},
			})
		if err != nil {
			t.Fatal("Failed to connect", err)
		}
		defer ldsr.Close()

		_, err = ldsr.WaitVersion(5*time.Second, collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind().String(), "")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("adscTLSDirect", func(t *testing.T) {
		testAdscTLS(t, creds)
	})

}

// testAdscTLS tests that ADSC helper can connect using TLS to Istiod
func testAdscTLS(t *testing.T, creds security.SecretManager) {
	// connect to the local XDS proxy - it's using a transient port.
	ldsr, err := adsc.Dial(util.MockPilotSGrpcAddr, "",
		&adsc.Config{
			IP:        "10.11.10.1",
			Namespace: "test",
			Secrets:   creds,
			Watch: []string{
				v3.ClusterType,
				xds.TypeURLConnections,
				collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind().String()},
		})
	if err != nil {
		t.Fatal("Failed to connect", err)
	}
	defer ldsr.Close()
}

func TestInternalEvents(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

	ads := s.Connect(
		&model.Proxy{
			Metadata: &model.NodeMetadata{
				Generator: "event",
			},
		},
		[]string{xds.TypeURLConnections},
		[]string{},
	)
	defer ads.Close()

	dr, err := ads.WaitVersion(5*time.Second, xds.TypeURLConnections, "")
	if err != nil {
		t.Fatal(err)
	}

	if dr.Resources == nil || len(dr.Resources) == 0 {
		t.Error("No data")
	}

	// Create a second connection - we should get an event.s
	ads2 := s.Connect(nil, nil, nil)
	defer ads2.Close()

	dr, err = ads.WaitVersion(5*time.Second, xds.TypeURLConnections,
		dr.VersionInfo)
	if err != nil {
		t.Fatal(err)
	}
	if dr.Resources == nil || len(dr.Resources) == 0 {
		t.Fatal("No data")
	}
	t.Log(dr.Resources[0])

}

func TestAdsReconnectAfterRestart(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()
	err := sendEDSReq([]string{"fake-cluster"}, sidecarID(app3Ip, "app3"), "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("Expected EDS response, but go nil")
	}
	if len(res.Resources) != 1 || res.TypeUrl != v3.EndpointType {
		t.Fatalf("Expected one EDS resource, but got %v %s resources", len(res.Resources), res.TypeUrl)
	}

	// Close the connection and reconnect
	_ = adscon.CloseSend()
	adscon = s.ConnectADS()

	// Connect with empty resources.
	err = sendEDSReq([]string{}, sidecarID(app3Ip, "app3"), res.VersionInfo, res.Nonce, adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("Expected EDS response, but go nil")
	}
	if len(res.Resources) != 0 || res.TypeUrl != v3.EndpointType {
		t.Fatalf("Expected zero EDS resource, but got %v %s resources", len(res.Resources), res.TypeUrl)
	}
}

// Regression for envoy restart and overlapping connections
func TestAdsReconnectWithNonce(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()
	err := sendEDSReq([]string{"fake-cluster"}, sidecarID(app3Ip, "app3"), "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// closes old process and reconnect
	_ = adscon.CloseSend()
	adscon = s.ConnectADS()

	err = sendEDSReqReconnect([]string{"fake-cluster"}, adscon, res)
	if err != nil {
		t.Fatal(err)
	}
	err = sendEDSReq([]string{"fake-cluster"}, sidecarID(app3Ip, "app3"), "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, _ = adsReceive(adscon, 15*time.Second)

	if res == nil {
		t.Fatal("Expected EDS response, but go nil")
	}
	if len(res.Resources) != 1 || res.TypeUrl != v3.EndpointType {
		t.Fatalf("Expected one EDS resource, but got %v %s resources", len(res.Resources), res.TypeUrl)
	}
}

// Regression for envoy restart and overlapping connections
func TestAdsReconnect(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()
	err := sendCDSReq(sidecarID(app3Ip, "app3"), adscon)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = adsReceive(adscon, 15*time.Second)

	// envoy restarts and reconnects
	adscon2 := s.ConnectADS()
	err = sendCDSReq(sidecarID(app3Ip, "app3"), adscon2)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = adsReceive(adscon2, 15*time.Second)

	// closes old process
	adscon.CloseSend()

	time.Sleep(1 * time.Second)

	// event happens
	xds.AdsPushAll(s.Discovery)

	m, err := adsReceive(adscon2, 3*time.Second)
	if err != nil {
		t.Fatal("Recv failed", err)
	}
	if m == nil {
		t.Fatal("Expected CDS response, but go nil")
	}
	if len(m.Resources) == 0 || m.TypeUrl != v3.ClusterType {
		t.Fatalf("Expected non zero CDS resources, but got %v %s resources", len(m.Resources), m.TypeUrl)
	}
}

func TestAdsClusterUpdate(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()

	version := ""
	nonce := ""
	var sendEDSReqAndVerify = func(clusterName string) {
		err := sendEDSReq([]string{clusterName}, sidecarID("1.1.1.1", "app3"), version, nonce, adscon)
		if err != nil {
			t.Fatal(err)
		}
		res, err := adsReceive(adscon, 15*time.Second)
		version = res.VersionInfo
		nonce = res.Nonce
		if err != nil {
			t.Fatal("Recv failed", err)
		}

		if res.TypeUrl != v3.EndpointType {
			t.Errorf("Expecting %v got %v", v3.EndpointType, res.TypeUrl)
		}
		if res.Resources[0].TypeUrl != v3.EndpointType {
			t.Errorf("Expecting %v got %v", v3.EndpointType, res.Resources[0].TypeUrl)
		}

		cla, err := getLoadAssignment(res)
		if err != nil {
			t.Fatal("Invalid EDS response ", err)
		}
		if cla.ClusterName != clusterName {
			t.Error(fmt.Sprintf("Expecting %s got ", clusterName), cla.ClusterName)
		}
	}

	cluster1 := "outbound|80||local.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster1)
	cluster2 := "outbound|80||hello.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster2)
}

// nolint: lll
func TestAdsPushScoping(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

	const (
		svcSuffix = ".testPushScoping.com"
		ns1       = "ns1"
	)

	removeServiceByNames := func(ns string, names ...string) {
		configsUpdated := map[model.ConfigKey]struct{}{}

		for _, name := range names {
			hostname := host.Name(name)
			s.Discovery.MemRegistry.RemoveService(hostname)
			configsUpdated[model.ConfigKey{
				Kind:      gvk.ServiceEntry,
				Name:      string(hostname),
				Namespace: ns,
			}] = struct{}{}
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
		configsUpdated := map[model.ConfigKey]struct{}{}

		for _, name := range names {
			hostname := host.Name(name)
			configsUpdated[model.ConfigKey{
				Kind:      gvk.ServiceEntry,
				Name:      string(hostname),
				Namespace: ns,
			}] = struct{}{}

			s.Discovery.MemRegistry.AddService(hostname, &model.Service{
				Hostname: hostname,
				Address:  "10.11.0.1",
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
			s.Discovery.MemRegistry.AddEndpoint(hostname, "http-main", 2080, "192.168.1.10", i)
		}

		s.Discovery.ConfigUpdate(&model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
			{Kind: gvk.ServiceEntry, Name: string(hostname), Namespace: model.IstioDefaultConfigNamespace}: {},
		}})
	}

	addVirtualService := func(i int, hosts ...string) {
		if _, err := s.Store.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: gvk.VirtualService,
				Name:             fmt.Sprintf("vs%d", i), Namespace: model.IstioDefaultConfigNamespace},
			Spec: &networking.VirtualService{
				Hosts: hosts,
				Http: []*networking.HTTPRoute{{Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				}}},
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	removeVirtualService := func(i int) {
		s.Store.Delete(gvk.VirtualService, fmt.Sprintf("vs%d", i), model.IstioDefaultConfigNamespace)
	}
	addDestinationRule := func(i int, host string) {
		if _, err := s.Store.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: gvk.DestinationRule,
				Name:             fmt.Sprintf("dr%d", i), Namespace: model.IstioDefaultConfigNamespace},
			Spec: &networking.DestinationRule{
				Host:     host,
				ExportTo: nil,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	removeDestinationRule := func(i int) {
		s.Store.Delete(gvk.DestinationRule, fmt.Sprintf("dr%d", i), model.IstioDefaultConfigNamespace)
	}

	sc := &networking.Sidecar{
		Egress: []*networking.IstioEgressListener{
			{
				Hosts: []string{model.IstioDefaultConfigNamespace + "/*" + svcSuffix},
			},
		},
	}
	if _, err := s.Store.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: gvk.Sidecar,
			Name:             "sc", Namespace: model.IstioDefaultConfigNamespace},
		Spec: sc,
	}); err != nil {
		t.Fatal(err)
	}
	addService(model.IstioDefaultConfigNamespace, 1, 2, 3)

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
		}
		drIndexes []struct {
			index int
			host  string
		}

		expectUpdates   []string
		unexpectUpdates []string
	}
	svcCases := []svcCase{
		{
			desc:          "Add a scoped service",
			ev:            model.EventAdd,
			svcIndexes:    []int{4},
			ns:            model.IstioDefaultConfigNamespace,
			expectUpdates: []string{"lds"},
		}, // then: default 1,2,3,4
		{
			desc: "Add instances to a scoped service",
			ev:   model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{fmt.Sprintf("svc%d%s", 4, svcSuffix), []int{1, 2}}},
			ns:            model.IstioDefaultConfigNamespace,
			expectUpdates: []string{"eds"},
		}, // then: default 1,2,3,4
		{
			desc: "Add virtual service to a scoped service",
			ev:   model.EventAdd,
			vsIndexes: []struct {
				index int
				hosts []string
			}{{4, []string{fmt.Sprintf("svc%d%s", 4, svcSuffix)}}},
			expectUpdates: []string{"lds"},
		},
		{
			desc: "Delete virtual service of a scoped service",
			ev:   model.EventDelete,
			vsIndexes: []struct {
				index int
				hosts []string
			}{{index: 4}},
			expectUpdates: []string{"lds"},
		},
		{
			desc: "Add destination rule to a scoped service",
			ev:   model.EventAdd,
			drIndexes: []struct {
				index int
				host  string
			}{{4, fmt.Sprintf("svc%d%s", 4, svcSuffix)}},
			expectUpdates: []string{"cds"},
		},
		{
			desc: "Delete destination rule of a scoped service",
			ev:   model.EventDelete,
			drIndexes: []struct {
				index int
				host  string
			}{{index: 4}},
			expectUpdates: []string{"cds"},
		},
		{
			desc:            "Add a unscoped(name not match) service",
			ev:              model.EventAdd,
			svcNames:        []string{"foo.com"},
			ns:              model.IstioDefaultConfigNamespace,
			unexpectUpdates: []string{"cds"},
		}, // then: default 1,2,3,4, foo.com; ns1: 11
		{
			desc: "Add instances to an unscoped service",
			ev:   model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{"foo.com", []int{1, 2}}},
			ns:              model.IstioDefaultConfigNamespace,
			unexpectUpdates: []string{"eds"},
		}, // then: default 1,2,3,4
		{
			desc:            "Add a unscoped(ns not match) service",
			ev:              model.EventAdd,
			svcIndexes:      []int{11},
			ns:              ns1,
			unexpectUpdates: []string{"cds"},
		}, // then: default 1,2,3,4, foo.com; ns1: 11
		{
			desc: "Add virtual service to an unscoped service",
			ev:   model.EventAdd,
			vsIndexes: []struct {
				index int
				hosts []string
			}{{0, []string{"foo.com"}}},
			unexpectUpdates: []string{"cds"},
		},
		{
			desc: "Delete virtual service of a unscoped service",
			ev:   model.EventDelete,
			vsIndexes: []struct {
				index int
				hosts []string
			}{{index: 0}},
			unexpectUpdates: []string{"cds"},
		},
		{
			desc: "Add destination rule to an unscoped service",
			ev:   model.EventAdd,
			drIndexes: []struct {
				index int
				host  string
			}{{0, "foo.com"}},
			unexpectUpdates: []string{"cds"},
		},
		{
			desc: "Delete destination rule of a unscoped service",
			ev:   model.EventDelete,
			drIndexes: []struct {
				index int
				host  string
			}{{index: 0}},
			unexpectUpdates: []string{"cds"},
		},
		{
			desc:          "Remove a scoped service",
			ev:            model.EventDelete,
			svcIndexes:    []int{4},
			ns:            model.IstioDefaultConfigNamespace,
			expectUpdates: []string{"lds"},
		}, // then: default 1,2,3, foo.com; ns: 11
		{
			desc:            "Remove a unscoped(name not match) service",
			ev:              model.EventDelete,
			svcNames:        []string{"foo.com"},
			ns:              model.IstioDefaultConfigNamespace,
			unexpectUpdates: []string{"cds"},
		}, // then: default 1,2,3; ns1: 11
		{
			desc:            "Remove a unscoped(ns not match) service",
			ev:              model.EventDelete,
			svcIndexes:      []int{11},
			ns:              ns1,
			unexpectUpdates: []string{"cds"},
		}, // then: default 1,2,3
	}

	for _, c := range svcCases {
		t.Run(c.desc, func(t *testing.T) {
			// Let events from previous tests complete
			time.Sleep(time.Millisecond * 100)
			adscConn.WaitClear()
			var wantUpdates []string
			wantUpdates = append(wantUpdates, c.expectUpdates...)
			wantUpdates = append(wantUpdates, c.unexpectUpdates...)

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
						addVirtualService(vsIndex.index, vsIndex.hosts...)
					}
				}
				if len(c.drIndexes) > 0 {
					for _, drIndex := range c.drIndexes {
						addDestinationRule(drIndex.index, drIndex.host)
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
				if len(c.drIndexes) > 0 {
					for _, drIndex := range c.drIndexes {
						removeDestinationRule(drIndex.index)
					}
				}
			default:
				t.Fatalf("wrong event for case %v", c)
			}

			timeout := time.Second
			upd, _ := adscConn.Wait(timeout, wantUpdates...) // XXX slow for unexpect ...
			for _, expect := range c.expectUpdates {
				if !contains(upd, expect) {
					t.Fatalf("expected update %s not in updates %v", expect, upd)
				}
			}
			for _, unexpect := range c.unexpectUpdates {
				if contains(upd, unexpect) {
					t.Fatalf("expected to not get update %s, but it is in updates %v", unexpect, upd)
				}
			}
		})
	}
}

func TestAdsUpdate(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()

	s.Discovery.MemRegistry.AddService("adsupdate.default.svc.cluster.local", &model.Service{
		Hostname: "adsupdate.default.svc.cluster.local",
		Address:  "10.11.0.1",
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
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true})
	time.Sleep(time.Millisecond * 200)
	s.Discovery.MemRegistry.SetEndpoints("adsupdate.default.svc.cluster.local", "default",
		newEndpointWithAccount("10.2.0.1", "hello-sa", "v1"))

	err := sendEDSReq([]string{"outbound|2080||adsupdate.default.svc.cluster.local"}, sidecarID("1.1.1.1", "app3"), "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}

	res1, err := adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal("Recv failed", err)
	}

	if res1.TypeUrl != v3.EndpointType {
		t.Errorf("Expecting %v got %v", v3.EndpointType, res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != v3.EndpointType {
		t.Errorf("Expecting %v got %v", v3.EndpointType, res1.Resources[0].TypeUrl)
	}
	cla, err := getLoadAssignment(res1)
	if err != nil {
		t.Fatal("Invalid EDS response ", err)
	}

	ep := cla.Endpoints
	if len(ep) == 0 {
		t.Fatal("No endpoints")
	}
	lbe := ep[0].LbEndpoints
	if len(lbe) == 0 {
		t.Fatal("No lb endpoints")
	}
	if lbe[0].GetEndpoint().Address.GetSocketAddress().Address != "10.2.0.1" {
		t.Error("Expecting 10.2.0.1 got ", lbe[0].GetEndpoint().Address.GetSocketAddress().Address)
	}

	_ = s.Discovery.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.1.7.1", 1080)

	// will trigger recompute and push for all clients - including some that may be closing
	// This reproduced the 'push on closed connection' bug.
	xds.AdsPushAll(s.Discovery)

	res1, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}

	if res1.TypeUrl != v3.EndpointType {
		t.Errorf("Expecting %v got %v", v3.EndpointType, res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != v3.EndpointType {
		t.Errorf("Expecting %v got %v", v3.EndpointType, res1.Resources[0].TypeUrl)
	}
	_, err = getLoadAssignment(res1)
	if err != nil {
		t.Fatal("Invalid EDS response ", err)
	}
}

func TestEnvoyRDSProtocolError(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()

	err := sendRDSReq(gatewayID(gatewayIP), []string{routeA}, "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}

	xds.AdsPushAll(s.Discovery)

	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) != 1 {
		t.Fatal("No routes returned")
	}

	// send empty response and validate no routes are retuned.
	err = sendRDSReq(gatewayID(gatewayIP), nil, res.VersionInfo, res.Nonce, adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) != 0 {
		t.Fatalf("No routes expected but got routes %v", len(res.Resources))
	}

	// Refresh routes
	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA, routeB}, res.VersionInfo, res.Nonce, adscon)
	if err != nil {
		t.Fatal(err)
	}

	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes after protocol error")
	}
}

func TestEnvoyRDSUpdatedRouteRequest(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()

	err := sendRDSReq(gatewayID(gatewayIP), []string{routeA}, "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err := unmarshallRoute(res.Resources[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	xds.AdsPushAll(s.Discovery)

	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	if len(res.Resources) != 1 {
		t.Fatal("Expected only 1 route to be returned")
	}
	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil || len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	// Test update from A -> B
	err = sendRDSReq(gatewayID(gatewayIP), []string{routeB}, "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil || len(res.Resources) != 1 || route1.Name != routeB {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	// Test update from B -> A, B
	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA, routeB}, res.VersionInfo, res.Nonce, adscon)
	if err != nil {
		t.Fatal(err)
	}

	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes after protocol error")
	}
	if len(res.Resources) != 2 {
		t.Fatal("Expected 2 routes to be returned")
	}

	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	route2, err := unmarshallRoute(res.Resources[1].Value)
	if err != nil {
		t.Fatal(err)
	}

	if (route1.Name == routeA && route2.Name != routeB) || (route2.Name == routeA && route1.Name != routeB) {
		t.Fatal("Expected http.80 and https.443.http routes to be returned")
	}

	// Test update from B, B -> A

	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA}, "", "", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err = adsReceive(adscon, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}
}

func unmarshallRoute(value []byte) (*route.RouteConfiguration, error) {
	route := &route.RouteConfiguration{}

	err := proto.Unmarshal(value, route)
	if err != nil {
		return nil, err
	}
	return route, nil
}
