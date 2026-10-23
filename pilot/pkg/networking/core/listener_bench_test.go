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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
)

// buildHeadlessBenchmarkInstances builds n endpoints for a headless TCP service,
// exercising the per-endpoint calls to buildSidecarOutboundListener.
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

// BenchmarkOutboundListenersHeadlessService covers both headless layouts; "cidr" selects the
// combined listener. Two-service cases split the 1000 endpoints across services sharing a port.
func BenchmarkOutboundListenersHeadlessService(b *testing.B) {
	for _, cidrListener := range []bool{false, true} {
		b.Run(fmt.Sprintf("cidr=%t", cidrListener), func(b *testing.B) {
			test.SetForTest(b, &features.EnableHeadlessFilterChainListener, cidrListener)
			for _, numServices := range []int{1, 2} {
				counts := []int{1000, 2000, 4000}
				if numServices == 2 {
					counts = []int{1000}
				}
				for _, numVirtualServices := range counts {
					b.Run(fmt.Sprintf("services=%d/vs=%d/pods=1000", numServices, numVirtualServices), func(b *testing.B) {
						benchmarkHeadlessListeners(b, numServices, numVirtualServices, 1000)
					})
				}
			}
		})
	}
}

func benchmarkHeadlessListeners(b *testing.B, numServices, numVirtualServices, totalPods int) {
	const targetHost = "headless.default.svc.cluster.local"
	virtualServices := buildHeadlessBenchmarkVirtualServices(targetHost, numVirtualServices)
	services := make([]*model.Service, 0, numServices)
	instances := make([]*model.ServiceInstance, 0, totalPods)
	for i := 0; i < numServices; i++ {
		host := targetHost
		if i > 0 {
			host = fmt.Sprintf("headless-%d.default.svc.cluster.local", i)
			vs := virtualServices[i].Spec.(*networking.VirtualService)
			vs.Hosts = []string{host}
			vs.Tcp[0].Route[0].Destination.Host = host
		}
		svc := buildServiceWithPort(host, 9999, protocol.TCP, tnow)
		svc.Resolution = model.Passthrough
		svc.Attributes.ServiceRegistry = provider.Kubernetes
		services = append(services, svc)
		pods := buildHeadlessBenchmarkInstances(svc, totalPods/numServices)
		for _, pod := range pods {
			// Disjoint pod ranges, while both services contribute to the same listener port.
			pod.Endpoint.Addresses[0] = fmt.Sprintf("%d%s", 10+i, pod.Endpoint.Addresses[0][2:])
		}
		instances = append(instances, pods...)
	}
	cg := NewConfigGenTest(b, TestOptions{Services: services, Instances: instances, Configs: virtualServices})
	proxy := cg.SetupProxy(nil)
	push := cg.env.PushContext()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		NewListenerBuilder(proxy, push).buildSidecarOutboundListeners(proxy, push)
	}
}

// CIDR ServiceEntries on a shared port all add chains to the same wildcard listener.
func BenchmarkOutboundListenersCIDRServices(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("services=%d", count), func(b *testing.B) {
			services := make([]*model.Service, 0, count)
			for i := 0; i < count; i++ {
				services = append(services, buildService(fmt.Sprintf("cidr-%d.test", i),
					fmt.Sprintf("10.%d.%d.0/24", i/256, i%256), protocol.TCP, tnow))
			}
			cg := NewConfigGenTest(b, TestOptions{Services: services})
			p := cg.SetupProxy(getProxy())
			push := cg.env.PushContext()
			listeners := NewListenerBuilder(p, push).buildSidecarOutboundListeners(p, push)
			if len(listeners) != 1 || len(listeners[0].FilterChains) != count {
				b.Fatalf("expected one listener with %d chains", count)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				NewListenerBuilder(p, push).buildSidecarOutboundListeners(p, push)
			}
		})
	}
}
