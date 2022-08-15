// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package v1alpha3_test

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/pkg/xds"
)

const se = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se1
spec:
  hosts:
  - blah.somedomain
  addresses:
  - %s
  ports:
  - number: 9999
    name: TCP-9999
    protocol: TCP
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se2
spec:
  hosts:
  - blah.somedomain
  addresses:
  - %s
  ports:
  - number: 9999
    name: TCP-9999
    protocol: TCP
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vs1
spec:
  hosts:
  - blah.somedomain
  tls:
  - match:
    - port: 9999
      sniHosts:
      - blah.somedomain
    route:
    - destination:
        host: blah.somedomain
        port:
          number: 9999`

func TestServiceEntry(t *testing.T) {
	cases := []simulationTest{
		{
			name:       "identical CIDR (ignoreing insignificant bits) is dropped",
			config:     fmt.Sprintf(se, "1234:1f1:123:123:f816:3eff:feb8:2287/32", "1234:1f1:123:123:f816:3eff:febf:57ce/32"),
			kubeConfig: "",
			calls: []simulation.Expect{{
				// Expect listener, but no routing
				Name: "defined port",
				Call: simulation.Call{
					Port:       9999,
					HostHeader: "blah.somedomain",
					Address:    "1234:1f1:1:1:1:1:1:1",
					Protocol:   simulation.HTTP,
				},
				Result: simulation.Result{
					ListenerMatched: "0.0.0.0_9999",
					ClusterMatched:  "outbound|9999||blah.somedomain",
				},
			}},
		},
		{
			// TODO(https://github.com/istio/istio/issues/29197) we should probably drop these too
			name:       "overlapping CIDR causes multiple filter chain match",
			config:     fmt.Sprintf(se, "1234:1f1:123:123:f816:3eff:feb8:2287/16", "1234:1f1:123:123:f816:3eff:febf:57ce/32"),
			kubeConfig: "",
			calls: []simulation.Expect{{
				// Expect listener, but no routing
				Name: "defined port",
				Call: simulation.Call{
					Port:       9999,
					HostHeader: "blah.somedomain",
					Address:    "1234:1f1:1:1:1:1:1:1",
					Protocol:   simulation.HTTP,
				},
				Result: simulation.Result{
					Error:           simulation.ErrMultipleFilterChain,
					ListenerMatched: "0.0.0.0_9999",
				},
			}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{Metadata: &model.NodeMetadata{Labels: map[string]string{"app": "foo"}}}
			runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
				name:   tt.name,
				config: tt.config,
				calls:  tt.calls,
			})
		})
	}
}
