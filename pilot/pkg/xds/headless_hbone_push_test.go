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

package xds

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

const (
	headlessPushHost = "zk.zk.svc.cluster.local"
	headlessPushNS   = "zk"
)

type headlessPushPod struct {
	ip      string
	ambient bool // ambient-only pod; otherwise a sidecar pod
}

func headlessPushEndpoints(pods []headlessPushPod) []*model.IstioEndpoint {
	out := make([]*model.IstioEndpoint, 0, len(pods))
	for _, p := range pods {
		tlsMode := model.IstioMutualTLSModeLabel
		if p.ambient {
			tlsMode = model.DisabledTLSModeLabel
		}
		out = append(out, &model.IstioEndpoint{
			Addresses:       []string{p.ip},
			ServicePortName: "tcp-peer",
			EndpointPort:    7000,
			Labels:          map[string]string{model.TunnelLabel: model.TunnelHTTP},
			TLSMode:         tlsMode,
		})
	}
	return out
}

// headlessPushEnv returns an environment with one headless Kubernetes Service, and a function that sets its pods
// and builds the next push context the way a headless endpoint update does.
func headlessPushEnv(t *testing.T) func(pods []headlessPushPod) (*model.PushContext, *model.PushRequest) {
	svc := &model.Service{
		Hostname:       host.Name(headlessPushHost),
		DefaultAddress: constants.UnspecifiedIP,
		Ports:          model.PortList{{Name: "tcp-peer", Port: 7000, Protocol: protocol.TCP}},
		Resolution:     model.Passthrough,
		Attributes:     model.ServiceAttributes{Name: "zk", Namespace: headlessPushNS, ServiceRegistry: provider.Kubernetes},
	}
	env := model.NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(mesh.DefaultMeshConfig())
	env.ServiceDiscovery = memory.NewServiceDiscovery(svc)
	store := model.NewFakeStore()
	env.ConfigStore = store
	stop := test.NewStop(t)
	env.VirtualServiceController = model.NewVirtualServiceController(
		store, model.VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler}, env.Watcher)
	go store.Run(stop)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("test", stop, store.HasSynced)
	kube.WaitForCacheSync("test", stop, env.VirtualServiceController.HasSynced)
	env.Init()

	var old *model.PushContext
	return func(pods []headlessPushPod) (*model.PushContext, *model.PushRequest) {
		env.EndpointIndex.UpdateServiceEndpoints(model.ShardKey{Cluster: "Kubernetes", Provider: provider.Kubernetes},
			headlessPushHost, headlessPushNS, headlessPushEndpoints(pods), false)
		req := &model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: headlessPushHost, Namespace: headlessPushNS}),
			Reason:         model.NewReasonStats(model.HeadlessEndpointUpdate),
		}
		pc := model.NewPushContext()
		pc.InitContext(env, old, req)
		req.Push = pc
		old = pc
		return pc, req
	}
}

func headlessPushProxy(nodeType model.NodeType, meta *model.NodeMetadata) *model.Proxy {
	if meta == nil {
		meta = &model.NodeMetadata{}
	}
	return &model.Proxy{
		Type:              nodeType,
		IPAddresses:       []string{"10.0.0.99"},
		ID:                "test-proxy.default",
		Metadata:          meta,
		MergedGateway:     &model.MergedGateway{},
		PrevMergedGateway: &model.PrevMergedGateway{},
	}
}

// With PILOT_ENABLE_HEADLESS_HBONE_SEND, a headless endpoint update pushes CDS to sidecars only when it changes the
// HBONE decision of the Service. Other headless endpoint updates keep the optimization from #60803.
func TestHeadlessHBONEEndpointUpdatePushesCDS(t *testing.T) {
	test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
	test.SetForTest(t, &features.EnableHBONESend, true)

	sidecars := []headlessPushPod{{ip: "10.0.0.1"}, {ip: "10.0.0.2"}, {ip: "10.0.0.3"}}
	sidecarsRestarted := []headlessPushPod{{ip: "10.0.0.1"}, {ip: "10.0.0.2"}, {ip: "10.0.0.13"}}
	oneAmbient := []headlessPushPod{{ip: "10.0.0.1"}, {ip: "10.0.0.2"}, {ip: "10.0.0.23", ambient: true}}
	twoAmbient := []headlessPushPod{{ip: "10.0.0.1"}, {ip: "10.0.0.22", ambient: true}, {ip: "10.0.0.23", ambient: true}}

	sidecar := headlessPushProxy(model.SidecarProxy, nil)
	steps := []struct {
		name string
		pods []headlessPushPod
		want bool
	}{
		{name: "all sidecars", pods: sidecars, want: false},
		{name: "a sidecar pod restarts", pods: sidecarsRestarted, want: false},
		{name: "first ambient pod", pods: oneAmbient, want: true},
		{name: "second ambient pod, decision unchanged", pods: twoAmbient, want: false},
		{name: "back to all sidecars", pods: sidecars, want: true},
	}
	next := headlessPushEnv(t)
	for _, s := range steps {
		_, req := next(s.pods)
		if _, got := cdsNeedsPush(req, sidecar); got != s.want {
			t.Errorf("%s: cdsNeedsPush() = %v, want %v", s.name, got, s.want)
		}
	}
}

func TestHeadlessHBONEEndpointUpdateCDSByProxy(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	sidecars := []headlessPushPod{{ip: "10.0.0.1"}, {ip: "10.0.0.2"}}
	mixed := []headlessPushPod{{ip: "10.0.0.1"}, {ip: "10.0.0.12", ambient: true}}

	for _, tt := range []struct {
		name  string
		flag  bool
		proxy *model.Proxy
		want  bool
	}{
		{name: "sidecar", flag: true, proxy: headlessPushProxy(model.SidecarProxy, nil), want: true},
		{name: "sidecar, flag off", flag: false, proxy: headlessPushProxy(model.SidecarProxy, nil), want: false},
		{
			name: "sidecar with DisableHBONESend", flag: true,
			proxy: headlessPushProxy(model.SidecarProxy, &model.NodeMetadata{DisableHBONESend: true}), want: false,
		},
		{name: "router", flag: true, proxy: headlessPushProxy(model.Router, nil), want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableHeadlessHBONESend, tt.flag)
			next := headlessPushEnv(t)
			next(sidecars)
			_, req := next(mixed)
			if _, got := cdsNeedsPush(req, tt.proxy); got != tt.want {
				t.Errorf("cdsNeedsPush() = %v, want %v", got, tt.want)
			}
		})
	}
}
