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
	config1 = &Config{
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

	config2 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{},
	}

	config3 = &Config{
		ConfigMeta: ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Bind:  "1.1.1.1",
					Hosts: []string{"foo/bar"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}

	services1 = []*Service{
		{Hostname: "foo"},
	}
)

func TestCreateSidecarScope(t *testing.T) {
	tests := []struct {
		name          string
		sidecarConfig *Config
		services      []*Service
	}{
		{
			"no-sidecar-config",
			nil,
			nil,
		},
		{
			"no-sidecar-config-with-service",
			nil,
			services1,
		},
		{
			"sidecar-with-multiple-egress",
			config1,
			nil,
		},
		{
			"sidecar-with-multiple-egress-with-service",
			config1,
			services1,
		},
		{
			"sidecar-with-zero-egress",
			config2,
			nil,
		},
		{
			"sidecar-with-multiple-egress_specific",
			config3,
			nil,
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			ps := NewPushContext()
			meshConfig := DefaultMeshConfig()
			ps.Env = &Environment{
				Mesh: &meshConfig,
			}
			configuredServices := 0
			if tt.services != nil {
				for _, s := range tt.services {
					ps.publicServices = append(ps.publicServices, s)
					configuredServices++
				}
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
				var found bool
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

			if configuredServices != len(sidecarScope.services) {
				t.Errorf("Expected %d services, Got: %d", configuredServices, len(sidecarScope.services))
			} else {
				for idx, s := range tt.services {
					if s.Hostname != sidecarScope.services[idx].Hostname {
						t.Errorf("Services expected: %v Got: %v", s.Hostname, sidecarScope.services[idx].Hostname)
					}
				}
			}
			// TODO destination rule
		})
	}
}
