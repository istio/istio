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

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
)

func TestDynamicSubset(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: app
  namespace: default
spec:
  host: app.default.svc.cluster.local
  trafficPolicy:
    loadBalancer:
      dynamicSubset:
        subsetSelectors:
        - keys:
          - version
          - stage
        - keys:
          - "%ip%"
        - keys:
          - "%hostname%"
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: other
  namespace: default
spec:
  host: other.default.svc.cluster.local
  trafficPolicy:
    loadBalancer:
      simple: ROUND_ROBIN
`,
		KubernetesObjectString: `
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
  name: other
  namespace: default
spec:
  clusterIP: 1.2.3.5
  selector:
    app: app
  ports:
  - name: http
    port: 80
---
apiVersion: v1
kind: Pod
metadata:
  name: app-v1
  namespace: default
  labels:
    app: app
    version: v1
spec: {}
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 10.0.0.1
  podIPs:
  - ip: 10.0.0.1
---
apiVersion: v1
kind: Pod
metadata:
  name: app-custom-hostname
  namespace: default
  labels:
    app: app
    version: v2
    stage: canary
spec:
  hostname: custom
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 10.0.0.2
  podIPs:
  - ip: 10.0.0.2
---
apiVersion: v1
kind: Pod
metadata:
  name: app-default
  namespace: default
  labels:
    app: app
spec: {}
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 10.0.0.3
  podIPs:
  - ip: 10.0.0.3
---
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: app
  namespace: default
  labels:
    kubernetes.io/service-name: app
addressType: IPv4
endpoints:
- addresses:
  - 10.0.0.1
  targetRef:
    kind: Pod
    name: app-v1
    namespace: default
- addresses:
  - 10.0.0.2
  targetRef:
    kind: Pod
    name: app-custom-hostname
    namespace: default
- addresses:
  - 10.0.0.3
  targetRef:
    kind: Pod
    name: app-default
    namespace: default
ports:
- name: http
  port: 80
---
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: other
  namespace: default
  labels:
    kubernetes.io/service-name: other
addressType: IPv4
endpoints:
- addresses:
  - 10.0.0.1
  targetRef:
    kind: Pod
    name: app-v1
    namespace: default
ports:
- name: http
  port: 80
`,
	})
	proxy := s.SetupProxy(&model.Proxy{ConfigNamespace: "default"})

	clusters := s.Clusters(proxy)
	c := xdstest.ExtractCluster("outbound|80||app.default.svc.cluster.local", clusters)
	if c == nil {
		t.Fatal("cluster not found")
	}
	assert.Equal(t, c.GetLbSubsetConfig(), &cluster.Cluster_LbSubsetConfig{
		FallbackPolicy: cluster.Cluster_LbSubsetConfig_ANY_ENDPOINT,
		SubsetSelectors: []*cluster.Cluster_LbSubsetConfig_LbSubsetSelector{
			{Keys: []string{"stage", "version"}},
			{Keys: []string{constants.DynamicSubsetIPKey}, SingleHostPerSubset: true},
			{Keys: []string{constants.DynamicSubsetHostnameKey}, SingleHostPerSubset: true},
		},
	})
	other := xdstest.ExtractCluster("outbound|80||other.default.svc.cluster.local", clusters)
	if other == nil {
		t.Fatal("cluster not found")
	}
	assert.Equal(t, other.GetLbSubsetConfig(), nil)

	// address -> envoy.lb metadata, per cluster
	got := map[string]map[string]map[string]string{}
	for _, c := range s.Endpoints(proxy) {
		eps := map[string]map[string]string{}
		for _, l := range c.Endpoints {
			for _, ep := range l.LbEndpoints {
				addr := ep.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
				md := map[string]string{}
				for k, v := range ep.GetMetadata().GetFilterMetadata()[constants.EnvoySubsetNamespace].GetFields() {
					md[k] = v.GetStringValue()
				}
				eps[addr] = md
			}
		}
		got[c.ClusterName] = eps
	}

	// Only the keys named by the destination rule are sent, and only the values the endpoint has.
	assert.Equal(t, got["outbound|80||app.default.svc.cluster.local"], map[string]map[string]string{
		"10.0.0.1": {"version": "v1", constants.DynamicSubsetIPKey: "10.0.0.1", constants.DynamicSubsetHostnameKey: "app-v1"},
		"10.0.0.2": {"version": "v2", "stage": "canary", constants.DynamicSubsetIPKey: "10.0.0.2", constants.DynamicSubsetHostnameKey: "custom"},
		"10.0.0.3": {constants.DynamicSubsetIPKey: "10.0.0.3", constants.DynamicSubsetHostnameKey: "app-default"},
	})
	// Services whose destination rule does not use dynamic subsets are unchanged.
	assert.Equal(t, got["outbound|80||other.default.svc.cluster.local"], map[string]map[string]string{
		"10.0.0.1": {},
	})
}
