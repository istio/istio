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

package meshnetwork

import (
	"fmt"
	"testing"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

const (
	VMService = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: vm
spec:
  hosts:
  - httpbin.com
  ports:
  - number: 7070
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
    labels:
      app: httpbin
    network: vm
`
)

var (
	i istio.Instance
	g galley.Instance
	p pilot.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("meshnetwork_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&i, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// Helm values from install/kubernetes/helm/istio/test-values/values-istio-mesh-networks.yaml
	cfg.ValuesFile = "test-values/values-istio-mesh-networks.yaml"
}

func TestAsymmetricMeshNetworkWithGatewayIP(t *testing.T) {
	framework.
		NewTest(t).
		Label(label.CustomSetup).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "meshnetwork",
				Inject: true,
			})
			// First setup the VM service and its endpoints
			if err := g.ApplyConfig(ns, VMService); err != nil {
				t.Fatal(err)
			}
			// Now setup a K8S service
			var instance echo.Instance
			echoConfig := echo.Config{
				Service:   "server",
				Namespace: ns,
				Pilot:     p,
				Galley:    g,
				Ports: []echo.Port{
					{
						Name:        "http",
						Protocol:    protocol.HTTP,
						ServicePort: 8080,
						// We use a port > 1024 to not require root
						InstancePort: 8080,
					},
				},
			}
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instance, echoConfig).
				BuildOrFail(t)
			if err := instance.WaitUntilCallable(instance); err != nil {
				t.Fatal(err)
			}

			vmSvcClusterName := "outbound|7070||httpbin.com"
			k8sSvcClusterName := fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local",
				echoConfig.Ports[0].ServicePort,
				echoConfig.Service, echoConfig.Namespace.Name())
			// Now get the EDS from the k8s pod to see if the VM IP is there.
			if err := checkEDSInPod(t, instance, vmSvcClusterName, "1.1.1.1"); err != nil {
				t.Fatal(err)
			}
			// Now get the EDS from the fake VM sidecar to see if the gateway IP is there for the echo service.
			// the Gateway IP:Port is set in the test-values/values-istio-mesh-networks.yaml
			if err := checkEDSInVM(t, ns.Name(), k8sSvcClusterName,
				"1.1.1.1", "2.2.2.2", 15443); err != nil {
				t.Fatal(err)
			}
		})
}

func checkEDSInPod(t *testing.T, c echo.Instance, vmSvcClusterName string, endpointIP string) error {
	accept := func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)

		if err := validator.
			Exists("{.configs[*].dynamicActiveClusters[?(@.cluster.name == '%s')]}", vmSvcClusterName).
			Check(); err != nil {
			return false, err
		}
		return true, nil
	}

	workloads, _ := c.Workloads()
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range workloads {
		if w.Sidecar() != nil {
			if err := w.Sidecar().WaitForConfig(accept, retry.Timeout(time.Second*10)); err != nil {
				return err
			}
			// Now that we have the desired cluster, get the cluster status to see if the VM IP made it to envoy
			clusters, err := w.Sidecar().Clusters()
			if err != nil {
				return err
			}
			for _, clusterStatus := range clusters.ClusterStatuses {
				if clusterStatus.Name == vmSvcClusterName {
					for _, host := range clusterStatus.HostStatuses {
						if host.Address != nil && host.Address.GetSocketAddress() != nil &&
							host.Address.GetSocketAddress().Address == endpointIP {
							t.Logf("found VM IP %s in envoy cluster %s", endpointIP, vmSvcClusterName)
							return nil
						}
					}
				}
			}
		}
	}

	return fmt.Errorf("could not find cluster %s on %s or cluster did not have VM IP %s",
		c.ID(), vmSvcClusterName, endpointIP)
}

func checkEDSInVM(t *testing.T, ns, k8sSvcClusterName, endpointIP, gatewayIP string, gatewayPort uint32) error {
	node := &model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{endpointIP},
		ID:              fmt.Sprintf("httpbin.com"),
		ConfigNamespace: ns,
		Metadata: &model.NodeMetadata{
			InstanceIPs:      []string{endpointIP},
			ConfigNamespace:  ns,
			Namespace:        ns,
			InterceptionMode: "NONE",
			Network:          "vm",
		},
	}

	// make an eds request, simulating a VM, asking for a cluster on k8s
	request := pilot.NewDiscoveryRequest(node.ServiceNode(), pilot.ClusterLoadAssignment)
	request.ResourceNames = []string{k8sSvcClusterName}
	if err := p.StartDiscovery(request); err != nil {
		return err
	}

	return p.WatchDiscovery(time.Second*10, func(resp *xdsapi.DiscoveryResponse) (b bool, e error) {
		for _, res := range resp.Resources {
			c := &xdsapi.ClusterLoadAssignment{}
			if err := proto.Unmarshal(res.Value, c); err != nil {
				return false, err
			}
			if c.ClusterName == k8sSvcClusterName {
				if len(c.Endpoints) != 1 || len(c.Endpoints[0].LbEndpoints) != 1 {
					return false, fmt.Errorf("unexpected EDS response: %s", c.String())
				}
				sockAddress := c.Endpoints[0].LbEndpoints[0].GetEndpoint().Address.GetSocketAddress()
				if sockAddress.Address != gatewayIP && sockAddress.GetPortValue() != gatewayPort {
					return false, fmt.Errorf("eds for VM does not have the expected IP:port (want %s:%d, got %s:%d)",
						gatewayIP, gatewayPort, sockAddress.Address, sockAddress.GetPortValue())
				}
				t.Logf("found gateway IP %s in envoy cluster %s", gatewayIP, k8sSvcClusterName)
				return true, nil
			}
		}
		return false, nil
	})
}
