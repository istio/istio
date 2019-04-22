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
	"fmt"
	"strings"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
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
			meshConfig := DefaultMeshConfig()
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
					for _, host := range egress.Hosts {
						parts := strings.SplitN(host, "/", 2)
						found = false
						for _, listeners := range sidecarScope.EgressListeners {
							if sidecarScopeHosts, ok := listeners.listenerHosts[parts[0]]; ok {
								for _, sidecarScopeHost := range sidecarScopeHosts {
									if sidecarScopeHost == Hostname(parts[1]) &&
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
							t.Errorf("Did not find %v entry in any listener", host)
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
