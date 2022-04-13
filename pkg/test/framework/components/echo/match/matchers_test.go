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

package match_test

import (
	"strconv"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	// 2 clusters on 2 networks
	cls1 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls1", Network: "n1", Index: 0, ClusterKind: cluster.Fake}}

	// simple pod
	a1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "a"}
	// simple pod with different svc
	b1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "b"}
	// virtual machine
	vm1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "vm", DeployAsVM: true}
	// headless
	headless1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "headless", Headless: true}
	// naked pod (uninjected)
	naked1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	// external svc
	external1 = &fakeInstance{
		Cluster: cls1, Namespace: namespace.Static("echo"), Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
			Annotations: map[echo.Annotation]*echo.AnnotationValue{echo.SidecarInject: {Value: strconv.FormatBool(false)}},
		}},
	}
)

func TestRegularPod(t *testing.T) {
	tests := []struct {
		app    echo.Instance
		expect bool
	}{
		{app: a1, expect: true},
		{app: b1, expect: true},
		{app: vm1, expect: false},
		{app: naked1, expect: false},
		{app: external1, expect: false},
		{app: headless1, expect: false},
	}
	for _, tt := range tests {
		t.Run(tt.app.Config().Service, func(t *testing.T) {
			if got := match.RegularPod(tt.app); got != tt.expect {
				t.Errorf("got %v expected %v", got, tt.expect)
			}
		})
	}
}

func TestNaked(t *testing.T) {
	tests := []struct {
		app    echo.Instance
		expect bool
	}{
		{app: a1, expect: false},
		{app: headless1, expect: false},
		{app: naked1, expect: true},
		{app: external1, expect: true},
	}
	for _, tt := range tests {
		t.Run(tt.app.Config().Service, func(t *testing.T) {
			if got := tt.app.Config().IsNaked(); got != tt.expect {
				t.Errorf("got %v expected %v", got, tt.expect)
			}
		})
	}
}

var _ echo.Instance = fakeInstance{}

// fakeInstance wraps echo.Config for test-framework internals tests where we don't actually make calls
type fakeInstance echo.Config

func (f fakeInstance) WithWorkloads(wl ...echo.Workload) echo.Instance {
	// TODO implement me
	panic("implement me")
}

func (f fakeInstance) Instances() echo.Instances {
	return echo.Instances{f}
}

func (f fakeInstance) ID() resource.ID {
	panic("implement me")
}

func (f fakeInstance) NamespacedName() echo.NamespacedName {
	return f.Config().NamespacedName()
}

func (f fakeInstance) PortForName(name string) echo.Port {
	return f.Config().Ports.MustForName(name)
}

func (f fakeInstance) Config() echo.Config {
	cfg := echo.Config(f)
	_ = cfg.FillDefaults(nil)
	return cfg
}

func (f fakeInstance) Address() string {
	panic("implement me")
}

func (f fakeInstance) Addresses() []string {
	panic("implement me")
}

func (f fakeInstance) Workloads() (echo.Workloads, error) {
	panic("implement me")
}

func (f fakeInstance) WorkloadsOrFail(test.Failer) echo.Workloads {
	panic("implement me")
}

func (f fakeInstance) MustWorkloads() echo.Workloads {
	panic("implement me")
}

func (f fakeInstance) Clusters() cluster.Clusters {
	panic("implement me")
}

func (f fakeInstance) Call(echo.CallOptions) (echo.CallResult, error) {
	panic("implement me")
}

func (f fakeInstance) CallOrFail(test.Failer, echo.CallOptions) echo.CallResult {
	panic("implement me")
}

func (f fakeInstance) Restart() error {
	panic("implement me")
}
