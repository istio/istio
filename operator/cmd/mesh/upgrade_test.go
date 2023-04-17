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

package mesh

import (
	"fmt"
	"testing"

	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

var expectedDiffFromAmbientToDemo = `components:
  cni:
    enabled: true -> false
  egressGateways:
    '[#0]':
      enabled: false -> true
      k8s: <empty> -> map[resources:map[requests:map[cpu:10m memory:40Mi]]] (ADDED)
  ingressGateways:
    '[#0]':
      enabled: false -> true
      k8s: <empty> -> map[resources:map[requests:map[cpu:10m memory:40Mi]] service:map[ports:[map[name:status-port
        port:15021 targetPort:15021] map[name:http2 port:80 targetPort:8080] map[name:https
        port:443 targetPort:8443] map[name:tcp port:31400 targetPort:31400] map[name:tls
        port:15443 targetPort:15443]]]] (ADDED)
  pilot:
    k8s: <empty> -> map[env:[map[name:PILOT_TRACE_SAMPLING value:100]] resources:map[requests:map[cpu:10m
      memory:100Mi]]] (ADDED)
  ztunnel: map[enabled:true] -> <empty> (REMOVED)
meshConfig:
  accessLogFile: <empty> -> /dev/stdout (ADDED)
  defaultConfig:
    proxyMetadata:
      ISTIO_META_ENABLE_HBONE: true -> <empty> (REMOVED)
  defaultProviders: map[metrics:[prometheus]] -> <empty> (REMOVED)
  extensionProviders:
    '[?->0]': <empty> -> map[envoyOtelAls:map[port:4317 service:opentelemetry-collector.istio-system.svc.cluster.local]
      name:otel] (ADDED)
    '[?->1]': <empty> -> map[name:skywalking skywalking:map[port:11800 service:tracing.istio-system.svc.cluster.local]]
      (ADDED)
    '[?->2]': <empty> -> map[name:otel-tracing opentelemetry:map[port:4317 service:opentelemetry-collector.otel-collector.svc.cluster.local]]
      (ADDED)
    '[0->?]': map[name:prometheus prometheus:map[]] -> <empty> (REMOVED)
profile: ambient -> demo
values:
  cni: map[ambient:map[enabled:true] excludeNamespaces:[kube-system] logLevel:info
    privileged:true] -> <empty> (REMOVED)
  gateways:
    istio-egressgateway:
      autoscaleEnabled: true -> false
    istio-ingressgateway:
      autoscaleEnabled: true -> false
  global:
    proxy:
      resources:
        requests:
          cpu: 100m -> 10m
          memory: 128Mi -> 40Mi
  pilot:
    autoscaleEnabled: true -> false
    env:
      CA_TRUSTED_NODE_ACCOUNTS: istio-system/ztunnel,kube-system/ztunnel -> <empty>
        (REMOVED)
      ENABLE_AUTO_SNI: true -> <empty> (REMOVED)
      PILOT_ENABLE_AMBIENT_CONTROLLERS: true -> <empty> (REMOVED)
      PILOT_ENABLE_HBONE: true -> <empty> (REMOVED)
      VERIFY_CERTIFICATE_AT_CLIENT: true -> <empty> (REMOVED)
  telemetry:
    enabled: false -> true
    v2:
      enabled: false -> true
`

func Test_compareIOPWithInstalledIOP(t *testing.T) {
	findOperatorInCluster = func(client dynamic.Interface, name, namespace string) (*v1alpha1.IstioOperator, error) {
		return generateIOP("ambient"), nil
	}

	diff, err := compareIOPWithInstalledIOP(kube.NewFakeClient(), generateIOP("demo"))
	assert.Equal(t, err, nil)
	assert.Equal(t, diff, expectedDiffFromAmbientToDemo)
}

func generateIOP(profile string) *v1alpha1.IstioOperator {
	demo, err := readFile(fmt.Sprintf("testdata/upgrade/generated-%s.yaml", profile))
	if err != nil {
		return nil
	}
	var iop *v1alpha1.IstioOperator
	if err := yaml.Unmarshal([]byte(demo), &iop); err != nil {
		return nil
	}
	return iop
}
