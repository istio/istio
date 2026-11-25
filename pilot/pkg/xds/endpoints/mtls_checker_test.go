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

package endpoints

import (
	"fmt"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	securityv1beta1 "istio.io/api/security/v1beta1"
	typev1beta1 "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	sdmemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

func newMtlsTestPush(t testing.TB, policies int) *model.PushContext {
	store := memory.NewController(collections.Pilot, true)
	for i := 0; i < policies; i++ {
		mode := []securityv1beta1.PeerAuthentication_MutualTLS_Mode{
			securityv1beta1.PeerAuthentication_MutualTLS_DISABLE,
			securityv1beta1.PeerAuthentication_MutualTLS_STRICT,
			securityv1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
		}[i%3]
		if _, err := store.Create(config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.PeerAuthentication, Name: fmt.Sprintf("pa-%d", i), Namespace: "ns"},
			Spec: &securityv1beta1.PeerAuthentication{
				Selector:      &typev1beta1.WorkloadSelector{MatchLabels: map[string]string{"app": fmt.Sprintf("svc-%d", i)}},
				Mtls:          &securityv1beta1.PeerAuthentication_MutualTLS{Mode: mode},
				PortLevelMtls: map[uint32]*securityv1beta1.PeerAuthentication_MutualTLS{8081: {Mode: securityv1beta1.PeerAuthentication_MutualTLS_DISABLE}},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	env := model.NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})
	env.ConfigStore = store
	env.ServiceDiscovery = sdmemory.NewServiceDiscovery()
	stop := test.NewStop(t)
	go store.Run(stop)
	env.VirtualServiceController = model.NewVirtualServiceController(store, model.VSControllerOptions{
		KrtDebugger: krt.GlobalDebugHandler,
		XDSUpdater:  model.NewEndpointIndexUpdater(env.EndpointIndex),
	}, env.Watcher)
	go env.VirtualServiceController.Run(stop)
	kube.WaitForCacheSync("vs", stop, env.VirtualServiceController.HasSynced)
	env.Init()
	push := model.NewPushContext()
	push.InitContext(env, nil, &model.PushRequest{Forced: true})
	return push
}

func TestMtlsCheckerSharesPoliciesAcrossEndpoints(t *testing.T) {
	push := newMtlsTestPush(t, 9)
	c := newMtlsChecker(push, push.AuthnPolicies, 8080, nil, "")
	ep := func(app string, port uint32) *model.IstioEndpoint {
		return &model.IstioEndpoint{
			Namespace: "ns", Labels: map[string]string{"app": app}, TLSMode: model.IstioMutualTLSModeLabel, EndpointPort: port,
		}
	}
	for i := 0; i < 2; i++ {
		// pa-0 DISABLE, pa-1 STRICT, pa-2 PERMISSIVE; port 8081 is DISABLE for all
		for _, tc := range []struct {
			ep   *model.IstioEndpoint
			want bool
		}{
			{ep("svc-0", 8080), false},
			{ep("svc-1", 8080), true},
			{ep("svc-2", 8080), true},
			{ep("svc-1", 8081), false},
		} {
			if got := c.checkMtlsEnabled(tc.ep, false); got != tc.want {
				t.Fatalf("pass %d: %v port %d: got %v want %v", i, tc.ep.Labels, tc.ep.EndpointPort, got, tc.want)
			}
		}
	}
}

func TestMtlsCheckerKeyIgnoresLabelOrder(t *testing.T) {
	push := newMtlsTestPush(t, 3)
	c := newMtlsChecker(push, push.AuthnPolicies, 8080, nil, "")
	for i := 0; i < 50; i++ {
		// Fresh maps each time so Go's randomized iteration order varies.
		c.checkMtlsEnabled(&model.IstioEndpoint{
			Namespace: "ns", TLSMode: model.IstioMutualTLSModeLabel, EndpointPort: 8080,
			Labels: map[string]string{"app": "svc-1", "version": "v1", "tier": "a", "zone": "z", "env": "prod", "team": "x"},
		}, false)
	}
	if got := len(c.policies); got != 1 {
		t.Fatalf("expected one cached policy for identical labels, got %d", got)
	}
}

func TestMtlsCheckerSkipsLabelsWithoutSelectorPolicies(t *testing.T) {
	push := newMtlsTestPush(t, 3)
	c := newMtlsChecker(push, push.AuthnPolicies, 8080, nil, "")
	ep := func(ns string, labels map[string]string) *model.IstioEndpoint {
		return &model.IstioEndpoint{Namespace: ns, Labels: labels, TLSMode: model.IstioMutualTLSModeLabel, EndpointPort: 8080}
	}
	// "other" has no PeerAuthentications, so labels cannot affect the result.
	c.checkMtlsEnabled(ep("other", map[string]string{"app": "a"}), false)
	c.checkMtlsEnabled(ep("other", map[string]string{"app": "b"}), false)
	if got := len(c.policies); got != 1 {
		t.Fatalf("expected one cached policy for namespace without selector policies, got %d", got)
	}
	c.checkMtlsEnabled(ep("ns", map[string]string{"app": "svc-0"}), false)
	c.checkMtlsEnabled(ep("ns", map[string]string{"app": "svc-1"}), false)
	if got := len(c.policies); got != 3 {
		t.Fatalf("expected per-label policies in namespace with selector policies, got %d", got)
	}
}

func BenchmarkCheckMtlsEnabled(b *testing.B) {
	for _, policies := range []int{0, 30, 300, 3000} {
		push := newMtlsTestPush(b, policies)
		for _, labelSets := range []int{1, 10, 100} {
			// One checker per cluster build, endpoints spread across labelSets distinct label sets.
			eps := make([]*model.IstioEndpoint, 1000)
			for i := range eps {
				lbls := map[string]string{"app": fmt.Sprintf("svc-%d", i%labelSets)}
				for j := 0; j < 9; j++ {
					lbls[fmt.Sprintf("label-%d", j)] = "v"
				}
				eps[i] = &model.IstioEndpoint{
					Namespace: "ns", Labels: lbls,
					TLSMode: model.IstioMutualTLSModeLabel, EndpointPort: 8080,
				}
			}
			b.Run(fmt.Sprintf("policies=%d/labelsets=%d", policies, labelSets), func(b *testing.B) {
				for n := 0; n < b.N; n++ {
					c := newMtlsChecker(push, push.AuthnPolicies, 8080, nil, "")
					for _, ep := range eps {
						c.checkMtlsEnabled(ep, false)
					}
				}
			})
		}
	}
}
