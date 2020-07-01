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

package meshnetwork

import (
	"fmt"
	"net"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/tests/integration/multicluster"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/proto"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
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
	multinetwork bool
	i            istio.Instance
	pilots       []pilot.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(func(ctx resource.Context) error {
			multinetwork = ctx.Environment().IsMultinetwork()
			return nil
		}).
		Setup(istio.Setup(&i, setupConfig)).
		Setup(multicluster.SetupPilots(&pilots)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}

	if multinetwork {
		// mesh networks will already be setup
		return
	}

	cfg.ControlPlaneValues = `
values:
  # overrides to test the meshNetworks.
  global:
    meshNetworks:
      # NOTE: DO NOT CHANGE THIS! Its hardcoded in Pilot in different areas
      Kubernetes:
        endpoints:
        - fromRegistry: Kubernetes
        gateways:
        - port: 15443
          address: 2.2.2.2
        vm: {}

    #This will cause ISTIO_META_NETWORK to be set on the pods and the
    #kube controller code to match endpoints from kubernetes with the default
    #cluster ID of "Kubernetes". Need to fix this code
    network: "Kubernetes"
`
}

const (
	servicePort = 8080
	serviceName = "server"
)

func TestAsymmetricMeshNetworkWithGatewayIP(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.expansion").
		Label(label.CustomSetup).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "meshnetwork",
				Inject: true,
			})

			// First setup the VM service and its endpoints
			if err := ctx.Config(ctx.Environment().ControlPlaneClusters()...).
				ApplyYAML(ns.Name(), VMService); err != nil {
				t.Fatal(err)
			}

			// Now setup a K8S service in each cluster
			instances := echoboot.NewBuilderOrFail(t, ctx).
				WithClusters(ctx.Clusters()).
				With(nil, echo.Config{
					Service:   serviceName,
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{}},
					SetupFn: func(cfg *echo.Config) {
						cfg.Pilot = pilots[cfg.Cluster.Index()]
					},
					Ports: []echo.Port{
						{
							Name:        "http",
							Protocol:    protocol.HTTP,
							ServicePort: servicePort,
							// We use a port > 1024 to not require root
							InstancePort: servicePort,
						},
					},
				}).BuildOrFail(t)

			for _, instance := range instances {
				if err := instance.WaitUntilCallable(instance); err != nil {
					t.Fatal(err)
				}
			}

			// all clusters' pods should discover the VM
			vmSvcClusterName := "outbound|7070||httpbin.com"
			ctx.NewSubTest("pod-eds").Run(func(ctx framework.TestContext) {
				for _, c := range ctx.Environment().ControlPlaneClusters() {
					c := c
					ctx.NewSubTest("cluster-%d", c.Index()).Run(func(ctx framework.TestContext) {
						ctx.NewSubTest("pod").Run(func(ctx framework.TestContext) {
							// Now get the EDS from the k8s pod to see if the VM IP is there.
							if err := checkEDSInPod(t, instances[c.Index()], vmSvcClusterName, "1.1.1.1"); err != nil {
								t.Fatal(err)
							}
						})

					})
				}
			})

			// There will be an ingress gateway for multinetwork traffic in each cluster
			gwAddrs := []net.TCPAddr{{IP: net.ParseIP("2.2.2.2"), Port: 15443}}
			if multinetwork {
				gwAddrs = []net.TCPAddr{}
				for _, c := range ctx.Clusters() {
					// TODO do I need the "minikube" check
					ing, err := ingress.New(ctx, ingress.Config{
						Istio:   i,
						Cluster: c,
					})
					if err != nil {
						ctx.Fatalf("failed initializing ingress for cluster %d: %v", c.Index(), err)
					}

					addr := ing.HTTPSAddress()
					if addr.Port == 0 {
						ctx.Fatalf("failed getting ingress address for cluster %d", c.Index())
					}
					gwAddrs = append(gwAddrs, addr)
				}
			}

			// For vms, we only test per-control-plane rather than every cluster. No matter which
			// control plane we query for discovery, all gateways should be discovered
			ctx.NewSubTest("vm-eds").Run(func(ctx framework.TestContext) {
				for _, cp := range ctx.Environment().ControlPlaneClusters() {
					p := pilots[cp.Index()]
					ctx.NewSubTest("cluster-%d", cp.Index()).Run(func(ctx framework.TestContext) {
						// Now get the EDS from the fake VM sidecar to see if the gateway IP is there for the echo service.
						// the Gateway IP:Port is set in the test-values/values-istio-mesh-networks.yaml from Pilots in all
						// control plane clusters
						k8sSvcClusterName := fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local",
							servicePort, serviceName, ns.Name())
						if err := checkEDSInVM(t, p, ns.Name(), k8sSvcClusterName,
							"1.1.1.1", gwAddrs); err != nil {
							ctx.Error(err)
						}
					})
				}
			})
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

func checkEDSInVM(t *testing.T, p pilot.Instance, ns, k8sSvcClusterName, endpointIP string, gatewayAddrs []net.TCPAddr) error {
	node := &model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{endpointIP},
		ID:              "httpbin.com",
		ConfigNamespace: ns,
		Metadata: &model.NodeMetadata{
			InstanceIPs:      []string{endpointIP},
			Namespace:        ns,
			InterceptionMode: "NONE",
			Network:          "vm",
		},
	}

	// make an eds request, simulating a VM, asking for a cluster on k8s
	request := pilot.NewDiscoveryRequest(node.ServiceNode(), v3.EndpointType)
	request.ResourceNames = []string{k8sSvcClusterName}
	if err := p.StartDiscovery(request); err != nil {
		return err
	}

	return p.WatchDiscovery(time.Second*10, func(resp *discovery.DiscoveryResponse) (b bool, e error) {
		for _, res := range resp.Resources {
			c := &endpoint.ClusterLoadAssignment{}
			if err := proto.Unmarshal(res.Value, c); err != nil {
				return false, err
			}
			if c.ClusterName == k8sSvcClusterName {
				if len(c.Endpoints) != 1 || len(c.Endpoints[0].LbEndpoints) != len(gatewayAddrs) {
					return false, fmt.Errorf("unexpected EDS response: %s", c.String())
				}

				found := 0
				foundMap := map[string]bool{}
				for _, addr := range gatewayAddrs {
					foundMap[addr.String()] = false
				}
				for _, lbEp := range c.Endpoints[0].LbEndpoints {
					sockAddress := lbEp.GetEndpoint().Address.GetSocketAddress()
					addr := net.JoinHostPort(sockAddress.Address, fmt.Sprint(sockAddress.GetPortValue()))
					if _, ok := foundMap[addr]; ok {
						foundMap[addr] = true
						found++
						t.Logf("found gateway address %s in envoy cluster %s", addr, k8sSvcClusterName)
					}
				}
				if found < len(foundMap) {
					return false, fmt.Errorf("all expected addresses not in eds response: %v", foundMap)
				}

				return true, nil
			}
		}
		return false, nil
	})
}
