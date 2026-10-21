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
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
)

// A cross-network inner tunnel forwarded by the ambient east-west gateway carries the service
// hostname as its CONNECT authority. With the bridge enabled, an HBONE-capable sidecar must route
// that authority to the matching target port on itself; with it disabled nothing changes.
func TestSidecarHBONEServiceAddressedConnect(t *testing.T) {
	test.SetForTest(t, &features.EnableSidecarHBONEListening, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)

	svc := buildServiceWithPort("echo.default.svc.cluster.local", 80, protocol.HTTP, tnow)
	inst := buildServiceInstance(svc, "1.1.1.1")
	inst.Endpoint.EndpointPort = 8080

	const (
		authority  = "echo.default.svc.cluster.local:80"
		podCluster = "main_internal_pod_8080"
	)

	for _, enabled := range []bool{false, true} {
		name := "bridge disabled"
		if enabled {
			name = "bridge enabled"
		}
		t.Run(name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSidecarAmbientBridge, enabled)
			cg := NewConfigGenTest(t, TestOptions{
				Services:  []*model.Service{svc},
				Instances: []*model.ServiceInstance{inst},
			})
			p := getProxy()
			p.Metadata.EnableHBONE = true
			proxy := cg.SetupProxy(p)

			var gotCluster bool
			for _, c := range cg.Clusters(proxy) {
				if c.Name == podCluster {
					gotCluster = true
					if !strings.Contains(c.String(), "1.1.1.1") || !strings.Contains(c.String(), "8080") {
						t.Errorf("cluster %s should re-enter main_internal targeting 1.1.1.1:8080, got %s", podCluster, c.String())
					}
				}
			}
			if gotCluster != enabled {
				t.Errorf("cluster %s present = %v, want %v", podCluster, gotCluster, enabled)
			}

			var terminate string
			for _, l := range cg.Listeners(proxy) {
				if l.Name == ConnectTerminate {
					terminate = l.String()
				}
			}
			if terminate == "" {
				t.Fatalf("no %s listener", ConnectTerminate)
			}
			gotRoute := strings.Contains(terminate, authority) && strings.Contains(terminate, podCluster)
			if gotRoute != enabled {
				t.Errorf("route %s -> %s present = %v, want %v", authority, podCluster, gotRoute, enabled)
			}
		})
	}
}
