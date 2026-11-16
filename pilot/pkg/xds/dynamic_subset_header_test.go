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
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	headertometadata "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/header_to_metadata/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
)

const dynamicSubsetHeaderServices = `
apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
spec:
  clusterIP: 1.2.3.4
  selector:
    app: app
  ports:
  - name: http
    port: 80
---
apiVersion: v1
kind: Service
metadata:
  name: plain
  namespace: default
spec:
  clusterIP: 1.2.3.5
  selector:
    app: plain
  ports:
  - name: http
    port: 80
`

func headerToMetadataFilter(t *testing.T, l *listener.Listener) *hcm.HttpFilter {
	t.Helper()
	if l == nil {
		t.Fatal("listener not found")
	}
	for _, fc := range l.FilterChains {
		for _, f := range xdstest.ExtractHTTPConnectionManager(t, fc).GetHttpFilters() {
			if f.Name == util.HeaderToMetadataFilter {
				return f
			}
		}
	}
	return nil
}

func TestDynamicSubsetHeader(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: app
  namespace: default
spec:
  hosts:
  - app.default.svc.cluster.local
  http:
  - name: selected
    match:
    - uri:
        prefix: /selected
    route:
    - destination:
        host: app.default.svc.cluster.local
    headerToDynamicSubsetKeys:
    - header: x-version
      key: version
      defaultValue: v1
    - header: x-endpoint-ip
      key: "%ip%"
  - name: default
    route:
    - destination:
        host: app.default.svc.cluster.local
`,
		KubernetesObjectString: dynamicSubsetHeaderServices,
	})
	proxy := s.SetupProxy(&model.Proxy{ConfigNamespace: "default"})

	// The listener has the filter, disabled, so it has no effect unless a route enables it.
	filter := headerToMetadataFilter(t, xdstest.ExtractListener("0.0.0.0_80", s.Listeners(proxy)))
	if filter == nil {
		t.Fatal("header to metadata filter not found")
	}
	assert.Equal(t, filter.Disabled, true)

	// Only the route that selects a subset enables the filter, copying the headers into the subset metadata.
	got := map[string]*headertometadata.Config{}
	for _, vh := range xdstest.ExtractRouteConfigurations(s.Routes(proxy))["80"].GetVirtualHosts() {
		assert.Equal(t, vh.GetTypedPerFilterConfig()[util.HeaderToMetadataFilter], nil)
		for _, r := range vh.GetRoutes() {
			if cfg := r.GetTypedPerFilterConfig()[util.HeaderToMetadataFilter]; cfg != nil {
				got[vh.GetName()+"/"+r.GetName()] = xdstest.UnmarshalAny[headertometadata.Config](t, cfg)
			}
		}
	}
	assert.Equal(t, got, map[string]*headertometadata.Config{
		"app.default.svc.cluster.local:80/selected": {
			RequestRules: []*headertometadata.Config_Rule{
				{
					Header: "x-version",
					OnHeaderPresent: &headertometadata.Config_KeyValuePair{
						MetadataNamespace: constants.EnvoySubsetNamespace,
						Key:               "version",
						Type:              headertometadata.Config_STRING,
					},
					OnHeaderMissing: &headertometadata.Config_KeyValuePair{
						MetadataNamespace: constants.EnvoySubsetNamespace,
						Key:               "version",
						Value:             "v1",
						Type:              headertometadata.Config_STRING,
					},
				},
				{
					Header: "x-endpoint-ip",
					OnHeaderPresent: &headertometadata.Config_KeyValuePair{
						MetadataNamespace: constants.EnvoySubsetNamespace,
						Key:               constants.DynamicSubsetIPKey,
						Type:              headertometadata.Config_STRING,
					},
				},
			},
		},
	})
}

// The filter is only added to listeners when a virtual service uses the feature.
func TestDynamicSubsetHeaderUnused(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{KubernetesObjectString: dynamicSubsetHeaderServices})
	proxy := s.SetupProxy(&model.Proxy{ConfigNamespace: "default"})
	assert.Equal(t, headerToMetadataFilter(t, xdstest.ExtractListener("0.0.0.0_80", s.Listeners(proxy))), nil)
}
