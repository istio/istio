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

// TestPeerAuthenticationPassthrough tests the PeerAuthentication policy applies correctly on the passthrough filter chain,
// including both global configuration and port level configuration.
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
		r := simulation.Call{Protocol: simulation.HTTP, Port: port, CallMode: simulation.CallModeInbound, TLS: tls}
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
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
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
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
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
					Name: "mtls on port 8000",
					Call: mkCall(8000, simulation.MTLS),
					// This will send an mTLS request to plaintext HTTP port, which is expected to fail
					Result: simulation.Result{Error: simulation.ErrProtocolError},
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
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
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
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
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
					Name: "mtls on port 8000",
					Call: mkCall(8000, simulation.MTLS),
					// We match the plaintext HTTP filter chain, which is a protocol error (as expected)
					Result: simulation.Result{Error: simulation.ErrProtocolError},
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
	proxy := &model.Proxy{
		Labels:   map[string]string{"app": "foo"},
		Metadata: &model.NodeMetadata{Labels: map[string]string{"app": "foo"}},
	}
	for _, tt := range cases {
		runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
			name:   tt.name,
			config: tt.config,
			calls:  tt.calls,
		})
	}
}

// TestPeerAuthenticationWithSidecar tests the PeerAuthentication policy applies correctly to filter chain generated from
// either the service or sidecar resource.
func TestPeerAuthenticationWithSidecar(t *testing.T) {
	pa := `
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
    9090:
      mode: DISABLE
---`
	sidecar := `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  labels:
    app: foo
  name: sidecar
spec:
  ingress:
  - defaultEndpoint: 127.0.0.1:8080
    port:
      name: tls
      number: 8080
      protocol: TCP
  - defaultEndpoint: 127.0.0.1:9090
    port:
      name: plaintext
      number: 9090
      protocol: TCP
  egress:
  - hosts:
    - "*/*"
  workloadSelector:
    labels:
      app: foo
---`
	partialSidecar := `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  labels:
    app: foo
  name: sidecar
spec:
  ingress:
  - defaultEndpoint: 127.0.0.1:8080
    port:
      name: tls
      number: 8080
      protocol: TCP
  egress:
  - hosts:
    - "*/*"
  workloadSelector:
    labels:
      app: foo
---`
	instancePorts := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - foo.bar
  endpoints:
  - address: 1.1.1.1
    labels:
      app: foo
  location: MESH_INTERNAL
  resolution: STATIC
  ports:
  - name: tls
    number: 8080
    protocol: TCP
  - name: plaintext
    number: 9090
    protocol: TCP
---`
	instanceNoPorts := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - foo.bar
  endpoints:
  - address: 1.1.1.1
    labels:
      app: foo
  location: MESH_INTERNAL
  resolution: STATIC
  ports:
  - name: random
    number: 5050
    protocol: TCP
---`
	mkCall := func(port int, tls simulation.TLSMode) simulation.Call {
		return simulation.Call{Protocol: simulation.TCP, Port: port, CallMode: simulation.CallModeInbound, TLS: tls}
	}
	cases := []struct {
		name   string
		config string
		calls  []simulation.Expect
	}{
		{
			name:   "service, no sidecar",
			config: pa + instancePorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				{
					Name:   "plaintext on plaintext port",
					Call:   mkCall(9090, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// TLS is fine here; we are not sniffing TLS at all so anything is allowed
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
			},
		},
		{
			name:   "service, full sidecar",
			config: pa + sidecar + instancePorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				{
					Name:   "plaintext on plaintext port",
					Call:   mkCall(9090, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// TLS is fine here; we are not sniffing TLS at all so anything is allowed
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
			},
		},
		{
			name:   "no service, no sidecar",
			config: pa + instanceNoPorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name: "tls on tls port",
					Call: mkCall(8080, simulation.MTLS),
					// no ports defined, so we will passthrough
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name: "plaintext on plaintext port",
					Call: mkCall(9090, simulation.Plaintext),
					// no ports defined, so we will passthrough
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// no ports defined, so we will passthrough
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "no service, full sidecar",
			config: pa + sidecar + instanceNoPorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				{
					Name:   "plaintext on plaintext port",
					Call:   mkCall(9090, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// TLS is fine here; we are not sniffing TLS at all so anything is allowed
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
			},
		},
		{
			name:   "service, partial sidecar",
			config: pa + partialSidecar + instancePorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				// Despite being defined in the Service, we get no filter chain since its not in Sidecar
				{
					Name: "plaintext on plaintext port",
					Call: mkCall(9090, simulation.Plaintext),
					// port 9090 not defined in partialSidecar and will use plain text, plaintext request should pass.
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// no ports defined, so we will passthrough
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
	}
	proxy := &model.Proxy{
		Labels:   map[string]string{"app": "foo"},
		Metadata: &model.NodeMetadata{Labels: map[string]string{"app": "foo"}},
	}
	for _, tt := range cases {
		runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
			name:   tt.name,
			config: tt.config,
			calls:  tt.calls,
		})
	}
}
