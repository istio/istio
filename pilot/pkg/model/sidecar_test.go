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
	port8000 = []*Port{
		{
			Name: "port1",
			Port: 8000,
		},
	}

	port9000 = []*Port{
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
)

func TestCreateSidecarScope(t *testing.T) {
	tests := []struct {
		name          string
		sidecarConfig *Config
		// list of available service for a given proxy
		services []*Service
		// list of services expected to be in the listener
		excpectedServices []string
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
			[]string{"bar"},
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
			[]string{"bar"},
		},
		{
			"sidecar-with-multiple-egress-with-service-on-same-port",
			configs1,
			services3,
			[]string{"bar", "barprime"},
		},
		{
			"sidecar-with-multiple-egress-with-multiple-service",
			configs1,
			services4,
			[]string{"bar", "barprime"},
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
			[]string{"bar", "barprime"},
		},
		{
			"sidecar-with-multiple-egress-noport-with-services",
			configs3,
			services4,
			[]string{"bar", "barprime"},
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
					if string(s1.Hostname) == s2 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Unexpected service %v found in SidecarScope", s1.Hostname)
				}
			}

			for _, s1 := range tt.excpectedServices {
				found = false
				for _, s2 := range sidecarScope.services {
					if s1 == string(s2.Hostname) {
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
				t.Fatalf("Expected contains %v, got %v", got, tt.contains)
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
