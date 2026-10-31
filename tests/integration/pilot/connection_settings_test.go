//go:build integ

// Copyright Istio Authors.
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

package pilot

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	downstreamconnections "github.com/envoyproxy/go-control-plane/envoy/extensions/resource_monitors/downstream_connections/v3"

	"istio.io/api/annotation"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestConnectionSettings(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		t.NewSubTest("gateway xds").Run(testGatewayConnectionSettings)
		t.NewSubTest("bootstrap").Run(testBootstrapConnectionSettings)
	})
}

func testGatewayConnectionSettings(t framework.TestContext) {
	ingress := i.IngressFor(t.Clusters().Default())
	label := i.Settings().IngressGatewayIstioLabel
	if label == "" {
		label = "ingressgateway"
	}
	i.PatchMeshConfigOrFail(t, `
defaultConfig:
  connectionSettings:
    listenerPerConnectionBufferLimitBytes: 12345
    clusterPerConnectionBufferLimitBytes: 23456
    httpHeadersWithUnderscoresAction: HEADERS_WITH_UNDERSCORES_REJECT_REQUEST`)
	config := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: connection-settings
  namespace: %s
spec:
  selector:
    istio: %s
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - connection-settings.example.com
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: connection-settings
  namespace: %s
spec:
  hosts:
  - connection-settings.example.com
  gateways:
  - connection-settings
  http:
  - route:
    - destination:
        host: b
        port:
          number: 80
`, apps.Namespace.Name(), label, apps.Namespace.Name())
	t.ConfigIstio().YAML(apps.Namespace.Name(), config).ApplyOrFail(t)
	t.Cleanup(func() {
		t.ConfigIstio().YAML(apps.Namespace.Name(), config).DeleteOrFail(t)
	})

	pod, err := ingress.PodID(0)
	if err != nil {
		t.Fatal(err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		dump, err := t.Clusters().Default().EnvoyDoWithPort(context.Background(), pod, ingress.Namespace(), "GET", "config_dump", util.DefaultProxyAdminPort)
		if err != nil {
			return err
		}
		configDump := &admin.ConfigDump{}
		if err := protomarshal.Unmarshal(dump, configDump); err != nil {
			return err
		}
		return verifyGatewayConnectionSettings(configDump)
	})

	ingress.CallOrFail(t, echo.CallOptions{
		Port:  echo.Port{Protocol: protocol.HTTP},
		HTTP:  echo.HTTP{Headers: headers.New().WithHost("connection-settings.example.com").With("x_test", "value").Build()},
		Check: check.Status(http.StatusBadRequest),
	})
}

func testBootstrapConnectionSettings(t framework.TestContext) {
	ns := namespace.NewOrFail(t, namespace.Config{Prefix: "connection-settings", Inject: true})
	workload := deployment.New(t).WithConfig(echo.Config{
		Namespace: ns,
		Service:   "connection-settings",
		Subsets: []echo.SubsetConfig{{
			Annotations: map[string]string{annotation.ProxyConfig.Name: `connectionSettings:
  globalDownstreamConnectionLimit: 7`},
		}},
	}).BuildOrFail(t)[0]

	if got := globalDownstreamConnectionLimit(workload.WorkloadsOrFail(t)[0].Sidecar().ConfigOrFail(t)); got != 7 {
		t.Fatalf("global downstream connection limit = %d, want 7", got)
	}
}

func verifyGatewayConnectionSettings(configDump *admin.ConfigDump) error {
	listenerFound := false
	clusterFound := false
	for _, config := range configDump.Configs {
		switch config.GetTypeUrl() {
		case "type.googleapis.com/envoy.admin.v3.ListenersConfigDump":
			dump := &admin.ListenersConfigDump{}
			if err := config.UnmarshalTo(dump); err != nil {
				return err
			}
			for _, dynamic := range dump.DynamicListeners {
				l := &listener.Listener{}
				if err := dynamic.GetActiveState().GetListener().UnmarshalTo(l); err != nil {
					return err
				}
				if l.GetPerConnectionBufferLimitBytes().GetValue() != 12345 {
					continue
				}
				for _, chain := range l.FilterChains {
					for _, filter := range chain.Filters {
						if filter.GetName() != "envoy.filters.network.http_connection_manager" {
							continue
						}
						manager := &hcm.HttpConnectionManager{}
						if err := filter.GetTypedConfig().UnmarshalTo(manager); err != nil {
							return err
						}
						if manager.GetCommonHttpProtocolOptions().GetHeadersWithUnderscoresAction().String() == "REJECT_REQUEST" {
							listenerFound = true
						}
					}
				}
			}
		case "type.googleapis.com/envoy.admin.v3.ClustersConfigDump":
			dump := &admin.ClustersConfigDump{}
			if err := config.UnmarshalTo(dump); err != nil {
				return err
			}
			for _, dynamic := range dump.DynamicActiveClusters {
				c := &cluster.Cluster{}
				if err := dynamic.GetCluster().UnmarshalTo(c); err != nil {
					return err
				}
				if c.GetPerConnectionBufferLimitBytes().GetValue() == 23456 {
					clusterFound = true
				}
			}
		}
	}
	if !listenerFound || !clusterFound {
		return fmt.Errorf("connection settings not applied: listener=%t cluster=%t", listenerFound, clusterFound)
	}
	return nil
}

func globalDownstreamConnectionLimit(configDump *admin.ConfigDump) int64 {
	for _, config := range configDump.Configs {
		if config.GetTypeUrl() != "type.googleapis.com/envoy.admin.v3.BootstrapConfigDump" {
			continue
		}
		bootstrap := &admin.BootstrapConfigDump{}
		if config.UnmarshalTo(bootstrap) != nil {
			return 0
		}
		for _, monitor := range bootstrap.GetBootstrap().GetOverloadManager().GetResourceMonitors() {
			if monitor.GetName() != "envoy.resource_monitors.global_downstream_max_connections" {
				continue
			}
			connections := &downstreamconnections.DownstreamConnectionsConfig{}
			if monitor.GetTypedConfig().UnmarshalTo(connections) == nil {
				return connections.GetMaxActiveDownstreamConnections()
			}
		}
	}
	return 0
}
