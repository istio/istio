// Copyright 2017 Istio Authors
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
)

func TestCreateSidecarScope(t *testing.T) {
	tests := []struct {
		name          string
		sidecarConfig *Config
		// list of available service for a given proxy
		services []*Service
		// list of services expected to be in the listener
		excpectedServices []*Service
	}{
		{
			"no-sidecar-config",
			nil,
			nil,
			nil,
		},
		{
			"no-sidecar-config-with-service",
			nil,
			services1,
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
		},
		{
			"sidecar-with-multiple-egress-with-service",
			configs1,
			services1,
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
		},
		{
			"sidecar-with-zero-egress-multiple-service",
			configs2,
			services4,
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport",
			configs3,
			nil,
			nil,
		},
		{
			"sidecar-with-multiple-egress-noport-with-specific-service",
			configs3,
			services2,
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
			[]*Service{
				{
					Hostname: "foo.svc.cluster.local",
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
			ps.Env = &Environment{
				Mesh: &meshConfig,
			}
			if tt.services != nil {
				ps.publicServices = append(ps.publicServices, tt.services...)
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

func TestContainsEgressNamespace(t *testing.T) {
	cases := []struct {
		name      string
		egress    []string
		namespace string
		contains  bool
	}{
		{"Just wildcard", []string{"*/*"}, "ns", true},
		{"Namespace and wildcard", []string{"ns/*", "*/*"}, "ns", true},
		{"Just Namespace", []string{"ns/*"}, "ns", true},
		{"Wrong Namespace", []string{"ns/*"}, "other-ns", false},
		{"No Sidecar", nil, "ns", true},
		{"No Sidecar Other Namespace", nil, "other-ns", false},
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
			ps.Env = &Environment{
				Mesh: &meshConfig,
			}

			services := []*Service{
				{Hostname: "nomatch", Attributes: ServiceAttributes{Namespace: "nomatch"}},
				{Hostname: "ns", Attributes: ServiceAttributes{Namespace: "ns"}},
			}
			ps.publicServices = append(ps.publicServices, services...)
			sidecarScope := ConvertToSidecarScope(ps, cfg, "default")
			if len(tt.egress) == 0 {
				sidecarScope = DefaultSidecarScopeForNamespace(ps, "default")
			}

			got := sidecarScope.DependsOnNamespace(tt.namespace)
			if got != tt.contains {
				t.Fatalf("Expected contains %v, got %v", tt.contains, got)
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
			ps.Env = &Environment{
				Mesh: &test.meshConfig,
			}

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
