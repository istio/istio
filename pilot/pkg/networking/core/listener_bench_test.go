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

package core

import (
	"fmt"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
)

// buildHeadlessBenchmarkInstances builds n pod endpoints for a headless/passthrough TCP
// service. This is the shape that makes buildSidecarOutboundListeners call
// buildSidecarOutboundListener (and therefore resolve the applicable VirtualServices via
// getConfigsForHost) once per pod endpoint - see listener.go's handling of
// model.Passthrough services with an unspecified address.
func buildHeadlessBenchmarkInstances(svc *model.Service, n int) []*model.ServiceInstance {
	instances := make([]*model.ServiceInstance, 0, n)
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
		instances = append(instances, buildServiceInstance(svc, ip))
	}
	return instances
}

// buildHeadlessBenchmarkVirtualServices builds n VirtualServices representative of a
// realistic sidecar egress scope: mostly hosts that don't match targetHost (so
// getConfigsForHost/host.Name.Matches has to scan through them), one VirtualService that
// does select targetHost, and a sprinkling of wildcarded hosts to exercise
// host.Name.IsWildCarded/Matches' wildcard branch.
func buildHeadlessBenchmarkVirtualServices(targetHost string, n int) []config.Config {
	configs := make([]config.Config, 0, n)
	for i := 0; i < n; i++ {
		vsHost := fmt.Sprintf("other-%d.default.svc.cluster.local", i)
		switch {
		case i == 0:
			vsHost = targetHost
		case i%10 == 0:
			vsHost = fmt.Sprintf("*.wild-%d.example.com", i)
		}
		configs = append(configs, config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             fmt.Sprintf("vs-%d", i),
				Namespace:        "default",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{vsHost},
				Tcp: []*networking.TCPRoute{{
					Route: []*networking.RouteDestination{{
						Destination: &networking.Destination{Host: vsHost},
					}},
				}},
			},
		})
	}
	return configs
}

// BenchmarkOutboundListenersHeadlessService reproduces the production hot path identified
// from a pilot-discovery CPU profile: a headless (Passthrough) Kubernetes TCP service whose
// buildSidecarOutboundListeners call fans out over every pod endpoint, with
// getConfigsForHost/host.Name.Matches/host.Name.IsWildCarded dominating CPU time. Run with
// -benchmem to also see the allocation cost of rebuilding svcConfigs per pod.
//
// The VirtualService counts (1000/2000/4000) match observed production sidecar egress scope
// size; 1000 pods matches our largest headless/StatefulSet endpoint counts.
func BenchmarkOutboundListenersHeadlessService(b *testing.B) {
	const targetHost = "headless.default.svc.cluster.local"
	const numPods = 1000

	for _, numVirtualServices := range []int{1000, 2000, 4000} {
		virtualServices := buildHeadlessBenchmarkVirtualServices(targetHost, numVirtualServices)
		b.Run(fmt.Sprintf("vs=%d/pods=%d", numVirtualServices, numPods), func(b *testing.B) {
			svc := buildServiceWithPort(targetHost, 9999, protocol.TCP, tnow)
			svc.Resolution = model.Passthrough
			svc.Attributes.ServiceRegistry = provider.Kubernetes

			instances := buildHeadlessBenchmarkInstances(svc, numPods)

			cg := NewConfigGenTest(b, TestOptions{
				Services:  []*model.Service{svc},
				Instances: instances,
				Configs:   virtualServices,
			})
			proxy := cg.SetupProxy(nil)
			push := cg.env.PushContext()

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				NewListenerBuilder(proxy, push).buildSidecarOutboundListeners(proxy, push)
			}
		})
	}
}
