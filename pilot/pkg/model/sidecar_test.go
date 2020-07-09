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

package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"

	"istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
)

var (
	port9999 = []*Port{
		{
			Name:     "uds",
			Port:     9999,
			Protocol: "HTTP",
		},
	}

	port7443 = []*Port{
		{
			Port:     7443,
			Protocol: "GRPC",
			Name:     "service-grpc-tls",
		},
	}

	port7442 = []*Port{
		{
			Port:     7442,
			Protocol: "HTTP",
			Name:     "http-tls",
		},
	}

	twoMatchingPorts = []*Port{
		{
			Port:     7443,
			Protocol: "GRPC",
			Name:     "service-grpc-tls",
		},
		{
			Port:     7442,
			Protocol: "HTTP",
			Name:     "http-tls",
		},
	}

	port8000 = []*Port{
		{
			Name:     "uds",
			Port:     8000,
			Protocol: "HTTP",
		},
	}

	port9000 = []*Port{
		{
			Name: "port1",
			Port: 9000,
		},
	}

	twoPorts = []*Port{
		{
			Name:     "uds",
			Port:     8000,
			Protocol: "HTTP",
		},
		{
			Name:     "uds",
			Port:     7000,
			Protocol: "HTTP",
		},
	}

	allPorts = []*Port{
		{
			Name:     "uds",
			Port:     8000,
			Protocol: "HTTP",
		},
		{
			Name:     "uds",
			Port:     7000,
			Protocol: "HTTP",
		},
		{
			Name: "port1",
			Port: 9000,
		},
	}

	configs1 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/*"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	configs2 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{},
	}

	configs3 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"foo/bar", "*/*"},
				},
			},
		},
	}

	configs4 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
			},
		},
	}

	configs5 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
			},
		},
	}

	configs6 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   8000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
				{
					Port: &networking.Port{
						Number:   7000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Hosts: []string{"foo/*"},
				},
			},
		},
	}

	configs7 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "sidecar-scope-ns1-ns2",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   23145,
						Protocol: "TCP",
						Name:     "outbound-tcp",
					},
					Bind: "7.7.7.7",
					Hosts: []string{"*/bookinginfo.com",
						"*/private.com",
					},
				},
				{
					Hosts: []string{"ns1/*",
						"*/*.tcp.com",
					},
				},
			},
		},
	}

	configs8 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "different-port-name",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "listener-grpc-tls",
					},
					Hosts: []string{"mesh/*"},
				},
			},
		},
	}

	configs9 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "sidecar-scope-wildcards",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "GRPC",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
				{
					Port: &networking.Port{
						Number:   7442,
						Protocol: "HTTP",
						Name:     "http-tls",
					},
					Hosts: []string{"ns2/*"},
				},
			},
		},
	}

	configs10 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "sidecar-scope-with-http-proxy",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	configs11 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "sidecar-scope-with-http-proxy-match-virtual-service",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo/virtualbar"},
				},
			},
		},
	}

	configs12 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "sidecar-scope-with-http-proxy-match-virtual-service-and-service",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo/virtualbar", "ns2/foo.svc.cluster.local"},
				},
			},
		},
	}

	configs13 = &Config{
		ConfigMeta: ConfigMeta{
			Name: "sidecar-scope-with-illegal-host",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "http_proxy",
						Name:     "grpc-tls",
					},
					Hosts: []string{"foo", "foo/bar"},
				},
			},
		},
	}
	services1 = []*Service{
		{Hostname: "bar"},
	}

	services2 = []*Service{
		{
			Hostname: "bar",
			Ports:    port8000,
		},
		{Hostname: "barprime"},
	}

	services3 = []*Service{
		{
			Hostname: "bar",
			Ports:    port9000,
		},
		{Hostname: "barprime"},
	}

	services4 = []*Service{
		{Hostname: "bar"},
		{Hostname: "barprime"},
	}

	services5 = []*Service{
		{
			Hostname: "bar",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
		{
			Hostname: "barprime",
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "foo",
			},
		},
	}

	services6 = []*Service{
		{
			Hostname: "bar",
			Ports:    twoPorts,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
	}

	services7 = []*Service{
		{
			Hostname: "bar",
			Ports:    twoPorts,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
		{
			Hostname: "barprime",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "foo",
			},
		},
		{
			Hostname: "foo",
			Ports:    allPorts,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "foo",
			},
		},
	}

	services8 = []*Service{
		{
			Hostname: "bookinginfo.com",
			Ports:    port9999,
			Attributes: ServiceAttributes{
				Name:      "bookinginfo.com",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "private.com",
			Attributes: ServiceAttributes{
				Name:      "private.com",
				Namespace: "ns1",
			},
		},
	}

	services9 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "mesh",
			},
		},
	}

	services10 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "bar.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "barprime.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "ns3",
			},
		},
	}

	services11 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
		{
			Hostname: "bar.svc.cluster.local",
			Ports:    twoMatchingPorts,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "barprime.svc.cluster.local",
			Ports:    port7442,
			Attributes: ServiceAttributes{
				Name:      "barprime",
				Namespace: "ns3",
			},
		},
	}

	services12 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns2",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services13 = []*Service{
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "ns1",
			},
		},
		{
			Hostname: "foo.svc.cluster.local",
			Ports:    port8000,
			Attributes: ServiceAttributes{
				Name:      "foo",
				Namespace: "mynamespace",
			},
		},
		{
			Hostname: "baz.svc.cluster.local",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "baz",
				Namespace: "ns3",
			},
		},
	}

	services14 = []*Service{
		{
			Hostname: "bar",
			Ports:    port7443,
			Attributes: ServiceAttributes{
				Name:      "bar",
				Namespace: "foo",
			},
		},
	}

	virtualServices1 = []Config{
		{
			ConfigMeta: ConfigMeta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
				Name:             "virtualbar",
				Namespace:        "foo",
			},
			Spec: &networking.VirtualService{
				Hosts: []string{"virtualbar"},
				Http: []*networking.HTTPRoute{
					{
						Mirror: &networking.Destination{Host: "foo.svc.cluster.local"},
						Route:  []*networking.HTTPRouteDestination{{Destination: &networking.Destination{Host: "baz.svc.cluster.local"}}},
					},
				},
			},
		},
	}
)

func TestCreateSidecarScope(t *testing.T) {
	tests := []struct {
		name          string
		sidecarConfig *Config
		// list of available service for a given proxy
		services        []*Service
		virtualServices []Config
		// list of services expected to be in the listener
		excpectedServices []*Service
	}{
		{
			"no-sidecar-config",
			nil,
			nil,
			nil,
			nil,
		},
		{
			"no-sidecar-config-with-service",
			nil,
			services1,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
			},
		},
		{
			"sidecar-with-multiple-egress",
			configs1,
			nil,
			nil,
			nil,
		},
		{
			"sidecar-with-multiple-egress-with-service",
			configs1,
			services1,
			nil,

			[]*Service{
				{
					Hostname: "bar",
				},
			},
		},
		{
			"sidecar-with-multiple-egress-with-service-on-same-port",
			configs1,
			services3,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
		},
		{
			"sidecar-with-multiple-egress-with-multiple-service",
			configs1,
			services4,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
		},
		{
			"sidecar-with-zero-egress",
			configs2,
			nil,
			nil,
			nil,
		},
		{
			"sidecar-with-zero-egress-multiple-service",
			configs2,
			services4,
			nil,
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport",
			configs3,
			nil,
			nil,
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport-with-specific-service",
			configs3,
			services2,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
		},
		{
			"sidecar-with-multiple-egress-noport-with-services",
			configs3,
			services4,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime",
				},
			},
		},
		{
			"sidecar-with-egress-port-match-with-services-with-and-without-port",
			configs4,
			services5,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
			},
		},
		{
			"sidecar-with-egress-port-trims-service-non-matching-ports",
			configs5,
			services6,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    port8000,
				},
			},
		},
		{
			"sidecar-with-egress-port-merges-service-ports",
			configs6,
			services6,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    twoPorts,
				},
			},
		},
		{
			"sidecar-with-egress-port-trims-and-merges-service-ports",
			configs6,
			services7,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    twoPorts,
				},
				{
					Hostname: "barprime",
					Ports:    port8000,
				},
				{
					Hostname: "foo",
					Ports:    twoPorts,
				},
			},
		},
		{
			"two-egresslisteners-one-with-port-and-without-port",
			configs7,
			services8,
			nil,
			[]*Service{
				{
					Hostname: "bookinginfo.com",
					Ports:    port9999,
				},
				{
					Hostname: "private.com",
				},
			},
		},
		// Validates when service is scoped to Sidecar, it uses service port rather than listener port.
		{
			"service-port-used-while-cloning",
			configs8,
			services9,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
			},
		},
		{
			"wild-card-egress-listener-match",
			configs9,
			services10,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "bar.svc.cluster.local",
					Ports:    port7442,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns2",
					},
				},
			},
		},
		{
			"wild-card-egress-listener-match-with-two-ports",
			configs9,
			services11,
			nil,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "bar.svc.cluster.local",
					Ports:    twoMatchingPorts,
					Attributes: ServiceAttributes{
						Name:      "bar",
						Namespace: "ns2",
					},
				},
			},
		},
		{
			"http-proxy-protocol-matches-any-port",
			configs10,
			services7,
			nil,
			[]*Service{
				{
					Hostname: "bar",
				},
				{
					Hostname: "barprime"},
				{
					Hostname: "foo",
				},
			},
		},
		{
			"virtual-service",
			configs11,
			services11,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
		},
		{
			"virtual-service-prefer-required",
			configs12,
			services12,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					// Ports should not be merged even though virtual service will select the service with 7443
					// as ns1 comes before ns2, because 8000 was already picked explicitly and is in different namespace
					Ports: port8000,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
		},
		{
			"virtual-service-prefer-config-namespace",
			configs11,
			services13,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port8000,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
		},
		{
			"virtual-service-pick-alphabetical",
			configs11,
			// Ambiguous; same hostname in ns1 and ns2, neither is config namespace
			// ns1 should always win
			services12,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
				{
					Hostname: "baz.svc.cluster.local",
					Ports:    port7443,
				},
			},
		},
		{
			"virtual-service-bad-host",
			configs11,
			services9,
			virtualServices1,
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
					Ports:    port7443,
				},
			},
		},
		{
			"sidecar-scope-with-illegal-host",
			configs13,
			services14,
			nil,
			[]*Service{
				{
					Hostname: "bar",
					Ports:    port7443,
				},
			},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			var found bool
			ps := NewPushContext()
			meshConfig := mesh.DefaultMeshConfig()
			ps.Mesh = &meshConfig
			if tt.services != nil {
				ps.publicServices = append(ps.publicServices, tt.services...)

				for _, s := range tt.services {
					if _, f := ps.ServiceByHostnameAndNamespace[s.Hostname]; !f {
						ps.ServiceByHostnameAndNamespace[s.Hostname] = map[string]*Service{}
					}
					ps.ServiceByHostnameAndNamespace[s.Hostname][s.Attributes.Namespace] = s
				}
			}
			if tt.virtualServices != nil {
				ps.publicVirtualServicesByGateway[constants.IstioMeshGateway] = append(ps.publicVirtualServicesByGateway[constants.IstioMeshGateway], tt.virtualServices...)
			}

			sidecarConfig := tt.sidecarConfig
			sidecarScope := ConvertToSidecarScope(ps, sidecarConfig, "mynamespace")
			configuredListeneres := 1
			if sidecarConfig != nil {
				r := sidecarConfig.Spec.(*networking.Sidecar)
				configuredListeneres = len(r.Egress)
			}

			numberListeners := len(sidecarScope.EgressListeners)
			if numberListeners != configuredListeneres {
				t.Errorf("Expected %d listeners, Got: %d", configuredListeneres, numberListeners)
			}

			if sidecarConfig != nil {
				a := sidecarConfig.Spec.(*networking.Sidecar)
				for _, egress := range a.Egress {
					for _, egressHost := range egress.Hosts {
						parts := strings.SplitN(egressHost, "/", 2)
						if len(parts) < 2 {
							continue
						}
						found = false
						for _, listeners := range sidecarScope.EgressListeners {
							if sidecarScopeHosts, ok := listeners.listenerHosts[parts[0]]; ok {
								for _, sidecarScopeHost := range sidecarScopeHosts {
									if sidecarScopeHost == host.Name(parts[1]) &&
										listeners.IstioListener.Port == egress.Port {
										found = true
										break
									}
								}
							}
							if found {
								break
							}
						}
						if !found {
							t.Errorf("Did not find %v entry in any listener", egressHost)
						}
					}
				}
			}

			for _, s1 := range sidecarScope.services {
				found = false
				for _, s2 := range tt.excpectedServices {
					if s1.Hostname == s2.Hostname {
						if len(s2.Ports) > 0 {
							if reflect.DeepEqual(s2.Ports, s1.Ports) {
								found = true
								break
							}
						} else {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("Unexpected service %v found in SidecarScope", s1.Hostname)
				}
			}

			for _, s1 := range tt.excpectedServices {
				found = false
				for _, s2 := range sidecarScope.services {
					if s1.Hostname == s2.Hostname {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected service %v in SidecarScope, but did not find it", s1)
				}
			}
			// TODO destination rule
		})
	}
}

func TestIstioEgressListenerWrapper(t *testing.T) {
	serviceA8000 := &Service{
		Hostname:   "host",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "a"},
	}
	serviceA9000 := &Service{
		Hostname:   "host",
		Ports:      port9000,
		Attributes: ServiceAttributes{Namespace: "a"},
	}
	serviceAalt := &Service{
		Hostname:   "alt",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "a"},
	}

	serviceB8000 := &Service{
		Hostname:   "host",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	serviceB9000 := &Service{
		Hostname:   "host",
		Ports:      port9000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	serviceBalt := &Service{
		Hostname:   "alt",
		Ports:      port8000,
		Attributes: ServiceAttributes{Namespace: "b"},
	}
	allServices := []*Service{serviceA8000, serviceA9000, serviceAalt, serviceB8000, serviceB9000, serviceBalt}

	tests := []struct {
		name          string
		listenerHosts map[string][]host.Name
		services      []*Service
		expected      []*Service
		namespace     string
	}{
		{
			name:          "*/* imports only those in a",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {wildcardService}},
			services:      allServices,
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
		{
			name:          "*/* will bias towards configNamespace",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {wildcardService}},
			services:      []*Service{serviceB8000, serviceB9000, serviceBalt, serviceA8000, serviceA9000, serviceAalt},
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
		{
			name:          "a/* imports only those in a",
			listenerHosts: map[string][]host.Name{"a": {wildcardService}},
			services:      allServices,
			expected:      []*Service{serviceA8000, serviceA9000, serviceAalt},
			namespace:     "a",
		},
		{
			name:          "b/*, b/* imports only those in b",
			listenerHosts: map[string][]host.Name{"b": {wildcardService, wildcardService}},
			services:      allServices,
			expected:      []*Service{serviceB8000, serviceB9000, serviceBalt},
			namespace:     "a",
		},
		{
			name:          "*/alt imports alt in namespace a",
			listenerHosts: map[string][]host.Name{wildcardNamespace: {"alt"}},
			services:      allServices,
			expected:      []*Service{serviceAalt},
			namespace:     "a",
		},
		{
			name:          "b/alt imports alt in a namespaces",
			listenerHosts: map[string][]host.Name{"b": {"alt"}},
			services:      allServices,
			expected:      []*Service{serviceBalt},
			namespace:     "a",
		},
		{
			name:          "b/* imports doesn't import in namespace a with proxy in a",
			listenerHosts: map[string][]host.Name{"b": {wildcardService}},
			services:      []*Service{serviceA8000},
			expected:      []*Service{},
			namespace:     "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ilw := &IstioEgressListenerWrapper{
				listenerHosts: tt.listenerHosts,
			}
			got := ilw.selectServices(tt.services, tt.namespace)
			if !reflect.DeepEqual(got, tt.expected) {
				gots, _ := json.MarshalIndent(got, "", "  ")
				expecteds, _ := json.MarshalIndent(tt.expected, "", "  ")
				t.Errorf("Got %v, expected %v", string(gots), string(expecteds))
			}
		})
	}
}

func TestContainsEgressDependencies(t *testing.T) {
	const (
		svcName = "svc1.com"
		nsName  = "ns"
		drName  = "dr1"
		vsName  = "vs1"
	)

	allContains := func(ns string, contains bool) map[ConfigKey]bool {
		return map[ConfigKey]bool{
			{gvk.ServiceEntry, svcName, ns}:   contains,
			{gvk.VirtualService, vsName, ns}:  contains,
			{gvk.DestinationRule, drName, ns}: contains,
		}
	}

	cases := []struct {
		name   string
		egress []string

		contains map[ConfigKey]bool
	}{
		{"Just wildcard", []string{"*/*"}, allContains(nsName, true)},
		{"Namespace and wildcard", []string{"ns/*", "*/*"}, allContains(nsName, true)},
		{"Just Namespace", []string{"ns/*"}, allContains(nsName, true)},
		{"Wrong Namespace", []string{"ns/*"}, allContains("other-ns", false)},
		{"No Sidecar", nil, allContains("ns", true)},
		{"No Sidecar Other Namespace", nil, allContains("other-ns", false)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ConfigMeta: ConfigMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: &networking.Sidecar{
					Egress: []*networking.IstioEgressListener{
						{
							Hosts: tt.egress,
						},
					},
				},
			}
			ps := NewPushContext()
			meshConfig := mesh.DefaultMeshConfig()
			ps.Mesh = &meshConfig

			services := []*Service{
				{Hostname: "nomatch", Attributes: ServiceAttributes{Namespace: "nomatch"}},
				{Hostname: svcName, Attributes: ServiceAttributes{Namespace: nsName}},
			}
			virtualServices := []Config{
				{
					ConfigMeta: ConfigMeta{
						Name:      vsName,
						Namespace: nsName,
					},
					Spec: &networking.VirtualService{
						Hosts: []string{svcName},
					},
				},
			}
			destinationRules := []Config{
				{
					ConfigMeta: ConfigMeta{
						Name:      drName,
						Namespace: nsName,
					},
					Spec: &networking.DestinationRule{
						Host:     svcName,
						ExportTo: []string{"*"},
					},
				},
			}
			ps.publicServices = append(ps.publicServices, services...)
			ps.publicVirtualServicesByGateway[constants.IstioMeshGateway] = append(ps.publicVirtualServicesByGateway[constants.IstioMeshGateway], virtualServices...)
			ps.SetDestinationRules(destinationRules)
			sidecarScope := ConvertToSidecarScope(ps, cfg, "default")
			if len(tt.egress) == 0 {
				sidecarScope = DefaultSidecarScopeForNamespace(ps, "default")
			}

			for k, v := range tt.contains {
				if ok := sidecarScope.DependsOnConfig(k); ok != v {
					t.Fatalf("Expected contains %v-%v, but no match", k, v)
				}
			}
		})
	}
}

func TestSidecarOutboundTrafficPolicy(t *testing.T) {

	configWithoutOutboundTrafficPolicy := &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{},
	}
	configRegistryOnly := &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
	}
	configAllowAny := &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}

	meshConfigWithRegistryOnly, err := mesh.ApplyMeshConfigDefaults(`
outboundTrafficPolicy: 
  mode: REGISTRY_ONLY
`)
	if err != nil {
		t.Fatalf("unexpected error reading test mesh config: %v", err)
	}

	tests := []struct {
		name                  string
		meshConfig            v1alpha1.MeshConfig
		sidecar               *Config
		outboundTrafficPolicy *networking.OutboundTrafficPolicy
	}{
		{
			name:       "default MeshConfig, no Sidecar",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    nil,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
		{
			name:       "default MeshConfig, sidecar without OutboundTrafficPolicy",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    configWithoutOutboundTrafficPolicy,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
		{
			name:       "default MeshConfig, Sidecar with registry only",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    configRegistryOnly,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "default MeshConfig, Sidecar with allow any",
			meshConfig: mesh.DefaultMeshConfig(),
			sidecar:    configAllowAny,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
		{
			name:       "MeshConfig registry only, no Sidecar",
			meshConfig: *meshConfigWithRegistryOnly,
			sidecar:    nil,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "MeshConfig registry only, sidecar without OutboundTrafficPolicy",
			meshConfig: *meshConfigWithRegistryOnly,
			sidecar:    configWithoutOutboundTrafficPolicy,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "MeshConfig registry only, Sidecar with registry only",
			meshConfig: *meshConfigWithRegistryOnly,
			sidecar:    configRegistryOnly,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
		{
			name:       "MeshConfig registry only, Sidecar with allow any",
			meshConfig: *meshConfigWithRegistryOnly,
			sidecar:    configAllowAny,
			outboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ps := NewPushContext()
			ps.Mesh = &test.meshConfig

			var sidecarScope *SidecarScope
			if test.sidecar == nil {
				sidecarScope = DefaultSidecarScopeForNamespace(ps, "not-default")
			} else {
				sidecarScope = ConvertToSidecarScope(ps, test.sidecar, test.sidecar.Namespace)
			}

			if !reflect.DeepEqual(test.outboundTrafficPolicy, sidecarScope.OutboundTrafficPolicy) {
				t.Errorf("Unexpected sidecar outbound traffic, want %v, found %v",
					test.outboundTrafficPolicy, sidecarScope.OutboundTrafficPolicy)
			}

		})
	}
}
