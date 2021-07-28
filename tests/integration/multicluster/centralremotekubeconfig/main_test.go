// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package centralremotekubeconfig

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/multicluster"
)

var ist istio.Instance

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// Skip("https://github.com/istio/istio/pull/33045").
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(func(ctx resource.Context) error {
			// TODO, this should be exclusively configurable outside of the framework
			configCluster := ctx.Clusters()[1]
			externalControlPlaneCluster := ctx.Clusters()[0]
			for _, c := range ctx.Clusters() {
				c.(*kubecluster.Cluster).OverrideTopology(func(c cluster.Topology) cluster.Topology {
					return c.
						WithConfig(configCluster.Name()).
						WithPrimary(externalControlPlaneCluster.Name())
				})
			}
			return nil
		}).
		Setup(istio.Setup(&ist, func(_ resource.Context, cfg *istio.Config) {
			// Set the control plane values on the config.
			cfg.ConfigClusterValues = `
components:
  base:
    enabled: false
  pilot:
    enabled: false
  ingressGateways:
  - name: istio-ingressgateway
    enabled: false
  egressGateways:
  - name: istio-egressgateway
    enabled: false
  istiodRemote:
    enabled: true
values:
  global:
    externalIstiod: true
    omitSidecarInjectorConfigMap: true
    configCluster: true
  pilot:
    configMap: true`
			cfg.ControlPlaneValues = `
components:
  base:
    enabled: false
  pilot:
    enabled: true
    k8s:
      service:
        type: LoadBalancer
      overlays:
      - kind: Deployment
        name: istiod
        patches:
        - path: spec.template.spec.volumes[100]
          value: |-
            name: config-volume
            configMap:
              name: istio
        - path: spec.template.spec.volumes[100]
          value: |-
            name: inject-volume
            configMap:
              name: istio-sidecar-injector
        - path: spec.template.spec.containers[0].volumeMounts[100]
          value: |-
            name: config-volume
            mountPath: /etc/istio/config
        - path: spec.template.spec.containers[0].volumeMounts[100]
          value: |-
            name: inject-volume
            mountPath: /var/lib/istio/inject
      env:
      - name: INJECTION_WEBHOOK_CONFIG_NAME
        value: "istio-sidecar-injector-istio-system"
      - name: VALIDATION_WEBHOOK_CONFIG_NAME
        value: "istio-istio-system"
      - name: EXTERNAL_ISTIOD
        value: "true"
      - name: CLUSTER_ID
        value: remote
      - name: SHARED_MESH_CONFIG
        value: istio
  ingressGateways:
  - name: istio-ingressgateway
    enabled: false
  egressGateways:
  - name: istio-egressgateway
    enabled: false
values:
  global:
    operatorManageWebhooks: true
    configValidation: false
`
		})).
		Run()
}

func TestIngressGateway(t *testing.T) {
	multicluster.GatewayTest(t, "installation.multicluster.centralremotekubeconfig")
}
