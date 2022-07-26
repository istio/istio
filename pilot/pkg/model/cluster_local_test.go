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

package model_test

import (
	"testing"

	. "github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
)

func TestIsClusterLocal(t *testing.T) {
	cases := []struct {
		name     string
		m        *meshconfig.MeshConfig
		host     string
		expected bool
	}{
		{
			name:     "kube-system is local",
			m:        mesh.DefaultMeshConfig(),
			host:     "s.kube-system.svc.cluster.local",
			expected: true,
		},
		{
			name:     "api server local is local",
			m:        mesh.DefaultMeshConfig(),
			host:     "kubernetes.default.svc.cluster.local",
			expected: true,
		},
		{
			name:     "discovery server is local",
			m:        mesh.DefaultMeshConfig(),
			host:     "istiod.istio-system.svc.cluster.local",
			expected: true,
		},
		{
			name:     "not local by default",
			m:        mesh.DefaultMeshConfig(),
			host:     "not.cluster.local",
			expected: false,
		},
		{
			name: "override default namespace",
			m: &meshconfig.MeshConfig{
				// Remove the cluster-local setting for kube-system.
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
							ClusterLocal: false,
						},
						Hosts: []string{"*.kube-system.svc.cluster.local"},
					},
				},
			},
			host:     "s.kube-system.svc.cluster.local",
			expected: false,
		},
		{
			name: "override default service",
			m: &meshconfig.MeshConfig{
				// Remove the cluster-local setting for kube-system.
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
							ClusterLocal: false,
						},
						Hosts: []string{"kubernetes.default.svc.cluster.local"},
					},
				},
			},
			host:     "kubernetes.default.svc.cluster.local",
			expected: false,
		},
		{
			name: "local 1",
			m: &meshconfig.MeshConfig{
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
							ClusterLocal: true,
						},
						Hosts: []string{
							"*.ns1.svc.cluster.local",
							"*.ns2.svc.cluster.local",
						},
					},
				},
			},
			host:     "s.ns1.svc.cluster.local",
			expected: true,
		},
		{
			name: "local 2",
			m: &meshconfig.MeshConfig{
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
							ClusterLocal: true,
						},
						Hosts: []string{
							"*.ns1.svc.cluster.local",
							"*.ns2.svc.cluster.local",
						},
					},
				},
			},
			host:     "s.ns2.svc.cluster.local",
			expected: true,
		},
		{
			name: "not local",
			m: &meshconfig.MeshConfig{
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Settings: &meshconfig.MeshConfig_ServiceSettings_Settings{
							ClusterLocal: true,
						},
						Hosts: []string{
							"*.ns1.svc.cluster.local",
							"*.ns2.svc.cluster.local",
						},
					},
				},
			},
			host:     "s.ns3.svc.cluster.local",
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)

			env := &model.Environment{Watcher: mesh.NewFixedWatcher(c.m)}
			env.Init()

			clusterLocal := env.ClusterLocal().GetClusterLocalHosts().IsClusterLocal(host.Name(c.host))
			g.Expect(clusterLocal).To(Equal(c.expected))
		})
	}
}
