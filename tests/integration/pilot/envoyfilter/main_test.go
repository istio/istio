// Copyright 2019 Istio Authors
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

package envoyfilter

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"text/template"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
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
	EnvoyFilterConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: app
  namespace: {{.AppNamespace}}
spec:
  workloadSelector:
    labels:
      app: httpbin
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: SIDECAR_INBOUND
      listener:
        filterChain:
          filter:
            name: "envoy.http_connection_manager"
            subFilter:
              name: "mixer"
    patch:
      operation: INSERT_BEFORE
      value:
        name: envoy.lua
        config:
          inline_code: |
            function envoy_on_request(handle)
              handle:logWarn("DEBUG REQUEST")
            end
            function envoy_on_response(handle)
              handle:logWarn("DEBUG RESPONSE")
            end
  - applyTo: NETWORK_FILTER
    match:
      context: SIDECAR_INBOUND
      listener:
        portNumber: 80
        filterChain:
          filter:
            name: "envoy.http_connection_manager"
    patch:
      operation: MERGE
      value:
        typed_config:
          "@type": "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager"
          access_log:
          - name: envoy.http_grpc_access_log
            config: 
              common_config:
                log_name: "grpc-als-example"
                grpc_service:
                  envoy_grpc:
                    cluster_name: grpc-als-cluster
  - applyTo: CLUSTER
    match:
      context: SIDECAR_INBOUND
    patch:
      operation: ADD
      value:
        name: grpc-als-cluster
        type: STRICT_DNS
        connect_timeout: 0.25s
        http2_protocol_options: {}
        load_assignment:
          cluster_name: grpc-als-cluster
          endpoints:
          - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: 127.0.0.1
                    port_value: 9999
`

	AppConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - httpbin.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  endpoints:
  - address: 1.1.1.1
    labels:
      app: httpbin
`

	IncludedConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: dependency
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  endpoints:
  - address: 2.2.2.2
`
	PermissiveMtls = `
apiVersion: authentication.istio.io/v1alpha1
kind: Policy
metadata:
  name: default
  namespace: {{.AppNamespace}}
spec:
  peers:
  - mtls:
      mode: PERMISSIVE
`
	MeshConfig = `
disablePolicyChecks: false
mixerCheckServer: istio-policy.istio-system.svc.cluster.local:15004
mixerReportServer: istio-telemetry.istio-system.svc.cluster.local:15004
`
)

type Config struct {
	AppNamespace string
}

func setupTest(t *testing.T, ctx resource.Context, modifyConfig func(c Config) Config) (pilot.Instance, *model.Proxy) {
	meshConfig := mesh.DefaultMeshConfig()
	meshConfig.MixerCheckServer = "istio-policy.istio-system.svc.cluster.local:15004"
	meshConfig.MixerReportServer = "istio-telemetry.istio-system.svc.cluster.local:15004"

	g := galley.NewOrFail(t, ctx, galley.Config{MeshConfig: MeshConfig})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g, MeshConfig: &meshConfig})

	appNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "app",
		Inject: true,
	})

	config := modifyConfig(Config{
		AppNamespace: appNamespace.Name(),
	})

	// Apply all configs
	createConfig(t, g, config, EnvoyFilterConfig, appNamespace)
	createConfig(t, g, config, AppConfig, appNamespace)
	createConfig(t, g, config, IncludedConfig, appNamespace)
	createConfig(t, g, config, PermissiveMtls, appNamespace)

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

func TestMain(m *testing.M) {
	framework.
		NewSuite("envoyfilter_test", m).
		RequireEnvironment(environment.Native).
		Run()
}

func TestEnvoyFilterHTTPFilterInsertBefore(t *testing.T) {
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
		if err := p.WatchDiscovery(time.Second*500, checkHTTPFilter); err != nil {
			t.Error(err)
		}
	})
}

func checkHTTPFilter(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expected := map[string]struct{}{
		"1.1.1.1_80":      {},
		"0.0.0.0_80":      {},
		"virtualInbound":  {},
		"virtualOutbound": {},
	}

	expectedHTTPFilters := []string{"istio_authn", "envoy.lua", "mixer", "envoy.cors", "envoy.fault", "envoy.router"}
	expectedHTTPAccessLogFilteers := []string{"envoy.file_access_log", "envoy.http_grpc_access_log"}
	var listenerToCheck *xdsapi.Listener
	got := map[string]struct{}{}
	for _, res := range resp.Resources {
		c := &xdsapi.Listener{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}
		got[c.Name] = struct{}{}
		if c.Name == "1.1.1.1_80" {
			listenerToCheck = c
		}
	}
	if !reflect.DeepEqual(expected, got) {
		return false, fmt.Errorf("excepted listeners %+v, got %+v", expected, got)
	}

	// check for hcm, http filters
	for _, fc := range listenerToCheck.FilterChains {
		for _, networkFilter := range fc.Filters {
			if networkFilter.Name == wellknown.HTTPConnectionManager {
				hcm := &http_conn.HttpConnectionManager{}
				if networkFilter.GetTypedConfig() != nil {
					if err := ptypes.UnmarshalAny(networkFilter.GetTypedConfig(), hcm); err != nil {
						return false, fmt.Errorf("failed to unmarshall HCM (Any) from 1.1.1.1_80 listener: %v", err)
					}
				} else {
					// nolint: staticcheck
					if err := conversion.StructToMessage(networkFilter.GetConfig(), hcm); err != nil {
						return false, fmt.Errorf("failed to unmarshall HCM (Struct) from 1.1.1.1_80 listener: %v", err)
					}
				}

				if err := hcm.Validate(); err != nil {
					return false, fmt.Errorf("invalid http connection manager: %v", err)
				}
				httpFiltersFound := make([]string, 0)
				for _, httpFilter := range hcm.HttpFilters {
					httpFiltersFound = append(httpFiltersFound, httpFilter.Name)
				}
				if !reflect.DeepEqual(expectedHTTPFilters, httpFiltersFound) {
					return false, fmt.Errorf("excepted http filters %+v, got %+v",
						expectedHTTPFilters, httpFiltersFound)
				}

				accessLogFiltersFound := make([]string, 0)
				for _, al := range hcm.AccessLog {
					accessLogFiltersFound = append(accessLogFiltersFound, al.Name)
				}
				if !reflect.DeepEqual(expectedHTTPAccessLogFilteers, accessLogFiltersFound) {
					return false, fmt.Errorf("excepted accesslog filters %+v, got %+v",
						expectedHTTPAccessLogFilteers, accessLogFiltersFound)
				}

			}
		}
	}
	return true, nil
}
