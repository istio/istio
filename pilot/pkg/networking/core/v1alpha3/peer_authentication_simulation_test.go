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

package v1alpha3_test

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/pkg/xds"
)

func TestPeerAuthenticationPassthrough(t *testing.T) {
	paStrict := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
 name: default
spec:
 selector:
   matchLabels:
     app: foo
 mtls:
   mode: STRICT
---`
	paDisable := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
 name: default
spec:
 selector:
   matchLabels:
     app: foo
 mtls:
   mode: DISABLE
---`
	paPermissive := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
 name: default
spec:
 selector:
   matchLabels:
     app: foo
 mtls:
   mode: PERMISSIVE
---`
	paStrictWithDisableOnPort9000 := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
 name: default
spec:
 selector:
   matchLabels:
     app: foo
 mtls:
   mode: STRICT
 portLevelMtls:
   9000:
     mode: DISABLE
---`
	paDisableWithStrictOnPort9000 := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
 name: default
spec:
 selector:
   matchLabels:
     app: foo
 mtls:
   mode: DISABLE
 portLevelMtls:
   9000:
     mode: STRICT
---`
	paDisableWithPermissiveOnPort9000 := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  selector:
    matchLabels:
      app: foo
  mtls:
    mode: DISABLE
  portLevelMtls:
    9000:
      mode: PERMISSIVE
---`
	sePort8000 := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
 name: se
spec:
 hosts:
 - foo.bar
 endpoints:
 - address: 1.1.1.1
 location: MESH_INTERNAL
 resolution: STATIC
 ports:
 - name: http
   number: 8000
   protocol: HTTP
---`
	mkCall := func(port int, tls simulation.TLSMode) simulation.Call {
		// TODO https://github.com/istio/istio/issues/28506 address should not be required here
		r := simulation.Call{Port: port, CallMode: simulation.CallModeInbound, TLS: tls, Address: "1.1.1.1"}
		if tls == simulation.MTLS {
			r.Alpn = "istio"
		}
		return r
	}
	cases := []struct {
		name   string
		config string
		calls  []simulation.Expect
	}{
		{
			name:   "global disable",
			config: paDisable,
			calls: []simulation.Expect{
				{
					Name:   "mtls",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "plaintext",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "global strict",
			config: paStrict,
			calls: []simulation.Expect{
				{
					Name:   "plaintext",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "mtls",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "global permissive",
			config: paPermissive,
			calls: []simulation.Expect{
				{
					Name:   "plaintext",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "global disable and port 9000 strict",
			config: paDisableWithStrictOnPort9000,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on port 8000",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls on port 8000",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "plaintext port 9000",
					Call:   mkCall(9000, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "mtls port 9000",
					Call:   mkCall(9000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "global disable and port 9000 strict not in service",
			config: paDisableWithStrictOnPort9000 + sePort8000,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on port 8000",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|8000||"},
				},
				{
					Name:   "mtls on port 8000",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "plaintext port 9000",
					Call:   mkCall(9000, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "mtls port 9000",
					Call:   mkCall(9000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "global strict and port 9000 plaintext",
			config: paStrictWithDisableOnPort9000,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on port 8000",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "mtls on port 8000",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "plaintext port 9000",
					Call:   mkCall(9000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls port 9000",
					Call:   mkCall(9000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
			},
		},
		{
			name:   "global strict and port 9000 plaintext not in service",
			config: paStrictWithDisableOnPort9000 + sePort8000,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on port 8000",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "mtls on port 8000",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8000||"},
				},
				{
					Name:   "plaintext port 9000",
					Call:   mkCall(9000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls port 9000",
					Call:   mkCall(9000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
			},
		},
		{
			name:   "global plaintext and port 9000 permissive",
			config: paDisableWithPermissiveOnPort9000,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on port 8000",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls on port 8000",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "plaintext port 9000",
					Call:   mkCall(9000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls port 9000",
					Call:   mkCall(9000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "global plaintext and port 9000 permissive not in service",
			config: paDisableWithPermissiveOnPort9000 + sePort8000,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on port 8000",
					Call:   mkCall(8000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|8000||"},
				},
				{
					Name:   "mtls on port 8000",
					Call:   mkCall(8000, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "plaintext port 9000",
					Call:   mkCall(9000, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name:   "mtls port 9000",
					Call:   mkCall(9000, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
	}
	proxy := &model.Proxy{Metadata: &model.NodeMetadata{Labels: map[string]string{"app": "foo"}}}
	for _, tt := range cases {
		runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
			name:   tt.name,
			config: tt.config,
			calls:  tt.calls,
		})
	}
}
