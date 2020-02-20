// Copyright 2020 Istio Authors
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

package egressproxy

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"text/template"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	MeshConfig = `
disablePolicyChecks: false
mixerCheckServer: istio-policy.istio-system.svc.cluster.local:15004
mixerReportServer: istio-telemetry.istio-system.svc.cluster.local:15004
`
	Sidecar = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar-with-egressproxy
  namespace: {{.AppNamespace}}
spec:
  outboundTrafficPolicy:
    mode: ALLOW_ANY
    egress_proxy:
      host: foo.bar
      subset: shiny
      port:
        number: 5000
  egress:
  - hosts:
    - istio-config/*
`
)

type Config struct {
	AppNamespace string
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("outbound_traffic_policy_egressproxy_test", m).
		RequireEnvironment(environment.Native).
		Run()
}

func setupTest(t *testing.T, ctx resource.Context, modifyConfig func(c Config) Config) (pilot.Instance, *model.Proxy) {
	meshConfig := mesh.DefaultMeshConfig()

	g := galley.NewOrFail(t, ctx, galley.Config{MeshConfig: MeshConfig})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g, MeshConfig: &meshConfig})

	appNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "app",
		Inject: true,
	})

	config := modifyConfig(Config{
		AppNamespace: appNamespace.Name(),
	})

	// Apply sidecar config
	createConfig(t, g, config, Sidecar, appNamespace)

	time.Sleep(time.Second * 2)

	nodeID := &model.Proxy{
		ClusterID:       "integration-test",
		ID:              fmt.Sprintf("httpbin.%s", appNamespace.Name()),
		DNSDomain:       appNamespace.Name() + ".cluster.local",
		Type:            model.SidecarProxy,
		IPAddresses:     []string{"1.1.1.1"},
		ConfigNamespace: appNamespace.Name(),
	}
	return p, nodeID
}

func createConfig(t *testing.T, g galley.Instance, config Config, yaml string, namespace namespace.Instance) {
	tmpl, err := template.New("Config").Parse(yaml)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(namespace, buf.String()); err != nil {
		t.Fatalf("failed to apply config: %v. Config: %v", err, buf.String())
	}
}

func TestSidecarConfig(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		configFn := func(c Config) Config {
			return c
		}
		p, nodeID := setupTest(t, ctx, configFn)

		listenerReq := &xdsapi.DiscoveryRequest{
			Node: &xdscore.Node{
				Id: nodeID.ServiceNode(),
			},
			TypeUrl: v2.ListenerType,
		}

		if err := p.StartDiscovery(listenerReq); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(time.Second*500, checkFallThroughNetworkFilter); err != nil {
			t.Fatal(err)
		}

		routeReq := &xdsapi.DiscoveryRequest{
			Node: &xdscore.Node{
				Id: nodeID.ServiceNode(),
			},
			TypeUrl:       v2.RouteType,
			ResourceNames: []string{"99000"}, // random route name is being passed to trigger route generation in the RDS response
		}

		if err := p.StartDiscovery(routeReq); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(time.Second*500, checkFallThroughRouteConfig); err != nil {
			t.Fatal(err)
		}
	})
}

func checkFallThroughRouteConfig(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expectedEgressCluster := "outbound|5000|shiny|foo.bar"
	for _, res := range resp.Resources {
		rc := &xdsapi.RouteConfiguration{}
		if err := proto.Unmarshal(res.Value, rc); err != nil {
			return false, err
		}
		found := false
		for _, vh := range rc.GetVirtualHosts() {
			if vh.GetName() == "allow_any" {
				for _, r := range vh.GetRoutes() {
					if expectedEgressCluster == r.GetRoute().GetCluster() {
						found = true
						break
					}
				}
				break
			}
		}
		if !found {
			return false, fmt.Errorf("failed to find expected fallthrough route")
		}
	}
	return true, nil
}

func checkFallThroughNetworkFilter(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expected := map[string]struct{}{
		"virtualInbound":  {},
		"virtualOutbound": {},
	}

	expectedEgressCluster := "outbound|5000|shiny|foo.bar"
	var listenerToCheck *xdsapi.Listener
	got := map[string]struct{}{}
	for _, res := range resp.Resources {
		c := &xdsapi.Listener{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}

		got[c.Name] = struct{}{}
		if c.Name == "virtualOutbound" {
			listenerToCheck = c
		}
	}
	if !reflect.DeepEqual(expected, got) {
		return false, fmt.Errorf("excepted listeners %+v, got %+v", expected, got)
	}

	tcpproxyFilterFound := false
	for _, fc := range listenerToCheck.FilterChains {
		if fc.FilterChainMatch != nil {
			continue
		}
		for _, networkFilter := range fc.Filters {
			if networkFilter.Name == wellknown.TCPProxy {
				tcpproxyFilterFound = true
				tcpProxy := &tcp_proxy.TcpProxy{}
				if networkFilter.GetTypedConfig() != nil {
					if err := ptypes.UnmarshalAny(networkFilter.GetTypedConfig(), tcpProxy); err != nil {
						return false, fmt.Errorf("failed to unmarshall network filter (Passthrough) from virtualOutbound listener: %v", err)
					}
				}

				if err := tcpProxy.Validate(); err != nil {
					return false, fmt.Errorf("invalid tcp proxy network filter: %v", err)
				}
				egressClusterFound := tcpProxy.GetCluster()
				if !(egressClusterFound == expectedEgressCluster) {
					return false, fmt.Errorf("excepted egress cluster %+v, got %+v",
						expectedEgressCluster, egressClusterFound)
				}
			}
		}
	}
	if !tcpproxyFilterFound {
		return false, fmt.Errorf("failed to find tcpproxy network filter in the  virtualOutbound listener")
	}
	return true, nil
}
