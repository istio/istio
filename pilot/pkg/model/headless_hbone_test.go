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
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

type headlessTestPod struct {
	ip      string
	network network.ID
	sidecar bool // tlsMode=istio and tunnel=http
	ambient bool // tunnel=http only
}

func headlessTestPushContext(t *testing.T, old *PushContext, pods []headlessTestPod) (*PushContext, *Service) {
	t.Helper()
	svc := &Service{
		Hostname:       "zk.zk.svc.cluster.local",
		DefaultAddress: constants.UnspecifiedIP,
		Ports: PortList{
			{Name: "tcp-peer", Port: 7000, Protocol: protocol.TCP},
			{Name: "http-admin", Port: 8080, Protocol: protocol.HTTP},
		},
		Resolution: Passthrough,
		Attributes: ServiceAttributes{Namespace: "zk", ServiceRegistry: provider.Kubernetes},
	}
	var eps []*IstioEndpoint
	for _, p := range pods {
		labels := map[string]string{}
		tlsMode := DisabledTLSModeLabel
		if p.sidecar {
			tlsMode = IstioMutualTLSModeLabel
		}
		if p.sidecar || p.ambient {
			labels[TunnelLabel] = TunnelHTTP
		}
		eps = append(eps, &IstioEndpoint{
			Addresses: []string{p.ip}, ServicePortName: "tcp-peer", EndpointPort: 7000,
			Labels: labels, TLSMode: tlsMode, Network: p.network,
		})
	}
	env := NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})
	env.ServiceDiscovery = &localServiceDiscovery{services: []*Service{svc}}
	env.EndpointIndex.shardsBySvc = map[string]map[string]*EndpointShards{
		svc.Hostname.String(): {svc.Attributes.Namespace: {
			Shards: map[ShardKey][]*IstioEndpoint{{Cluster: "Kubernetes", Provider: provider.Kubernetes}: eps},
		}},
	}
	ps := NewPushContext()
	ps.Mesh = env.Mesh()
	ps.initServiceRegistry(env, nil)
	ps.initAmbient(env)
	ps.initHeadlessHBONE(old, true)
	return ps, svc
}

func TestHeadlessServiceNeedsHBONE(t *testing.T) {
	test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
	for _, tt := range []struct {
		name    string
		pods    []headlessTestPod
		prefer  bool
		network network.ID
		want    bool
	}{
		{name: "no pods", want: false},
		{name: "all sidecars", pods: []headlessTestPod{{ip: "1.0.0.1", sidecar: true}, {ip: "1.0.0.2", sidecar: true}}, want: false},
		{
			name: "all sidecars, PreferHBONESend", prefer: true,
			pods: []headlessTestPod{{ip: "1.0.0.1", sidecar: true}, {ip: "1.0.0.2", sidecar: true}}, want: true,
		},
		{name: "sidecar and ambient", pods: []headlessTestPod{{ip: "1.0.0.1", sidecar: true}, {ip: "1.0.0.2", ambient: true}}, want: true},
		{name: "all ambient", pods: []headlessTestPod{{ip: "1.0.0.1", ambient: true}, {ip: "1.0.0.2", ambient: true}}, want: true},
		{name: "a pod outside the mesh", pods: []headlessTestPod{{ip: "1.0.0.1", ambient: true}, {ip: "1.0.0.2"}}, want: false},
		{
			name: "pods on another network", network: "net1",
			pods: []headlessTestPod{{ip: "1.0.0.1", ambient: true, network: "net2"}, {ip: "1.0.0.2", ambient: true, network: "net2"}}, want: false,
		},
		{
			name: "pods on more than one network", network: "net1",
			pods: []headlessTestPod{{ip: "1.0.0.1", ambient: true, network: "net1"}, {ip: "1.0.0.2", ambient: true, network: "net2"}}, want: false,
		},
		{
			name: "all pods on the proxy network", network: "net1",
			pods: []headlessTestPod{{ip: "1.0.0.1", ambient: true, network: "net1"}, {ip: "1.0.0.2", sidecar: true, network: "net1"}}, want: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.PreferHBONESend, tt.prefer)
			ps, svc := headlessTestPushContext(t, nil, tt.pods)
			assert.Equal(t, ps.HeadlessServiceNeedsHBONE(svc, svc.Ports[0], tt.network), tt.want)
			// The precomputed state and the fallback must agree.
			assert.Equal(t, ps.computeHeadlessHBONEState(svc, 7000).sendHBONE(tt.network), tt.want)
			// HTTP ports are not precomputed.
			_, f := ps.headlessHBONE[headlessHBONEKey{service: svc.Key(), port: 8080}]
			assert.Equal(t, f, false)
		})
	}
}

func TestHeadlessHBONEChanged(t *testing.T) {
	key := sets.New(ConfigKey{Kind: kind.ServiceEntry, Name: "zk.zk.svc.cluster.local", Namespace: "zk"})
	sidecars := []headlessTestPod{{ip: "1.0.0.1", sidecar: true}, {ip: "1.0.0.2", sidecar: true}}
	restarted := []headlessTestPod{{ip: "1.0.0.1", sidecar: true}, {ip: "1.0.0.12", sidecar: true}}
	mixed := []headlessTestPod{{ip: "1.0.0.1", sidecar: true}, {ip: "1.0.0.22", ambient: true}}

	t.Run("flag off", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, false)
		ps, _ := headlessTestPushContext(t, nil, mixed)
		assert.Equal(t, ps.headlessHBONE == nil, true)
		assert.Equal(t, ps.HeadlessHBONEChanged(key), false)
	})

	test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
	p1, _ := headlessTestPushContext(t, nil, sidecars)
	assert.Equal(t, p1.HeadlessHBONEChanged(key), false)
	p2, _ := headlessTestPushContext(t, p1, restarted)
	assert.Equal(t, p2.HeadlessHBONEChanged(key), false)
	p3, _ := headlessTestPushContext(t, p2, mixed)
	assert.Equal(t, p3.HeadlessHBONEChanged(key), true)
	// Another Service is not affected.
	assert.Equal(t, p3.HeadlessHBONEChanged(sets.New(ConfigKey{Kind: kind.ServiceEntry, Name: "other", Namespace: "zk"})), false)
	p4, _ := headlessTestPushContext(t, p3, mixed)
	assert.Equal(t, p4.HeadlessHBONEChanged(key), false)

	// Without a service change, the state is copied and nothing changed.
	p5 := NewPushContext()
	p5.initHeadlessHBONE(p4, false)
	assert.Equal(t, len(p5.headlessHBONE), len(p4.headlessHBONE))
	assert.Equal(t, p5.HeadlessHBONEChanged(key), false)
}
