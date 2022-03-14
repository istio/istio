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

package echotest

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	// TODO set this up with echobuilder/cluster builder in Fake mode

	// 2 clusters on 2 networks
	cls1 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls1", Network: "n1", Index: 0, ClusterKind: cluster.Fake}}
	cls2 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls2", Network: "n2", Index: 1, ClusterKind: cluster.Fake}}

	// simple pod
	a1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "a"}
	a2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo"), Service: "a"}
	// simple pod with different svc
	b1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "b"}
	b2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo"), Service: "b"}
	// another simple pod with different svc
	c1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "c"}
	c2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo"), Service: "c"}
	// simple pod with a different namespace
	aNs1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo2"), Service: "a"}
	aNs2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo2"), Service: "a"}
	// virtual machine
	vm1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "vm", DeployAsVM: true}
	vm2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo"), Service: "vm", DeployAsVM: true}
	// headless
	headless1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "headless", Headless: true}
	headless2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo"), Service: "headless", Headless: true}
	// naked pod (uninjected)
	naked1 = &fakeInstance{Cluster: cls1, Namespace: namespace.Static("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	naked2 = &fakeInstance{Cluster: cls2, Namespace: namespace.Static("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	// external svc
	external1 = &fakeInstance{
		Cluster: cls1, Namespace: namespace.Static("echo"), Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
			Annotations: map[echo.Annotation]*echo.AnnotationValue{echo.SidecarInject: {Value: strconv.FormatBool(false)}},
		}},
	}
	external2 = &fakeInstance{
		Cluster: cls2, Namespace: namespace.Static("echo"), Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
			Annotations: map[echo.Annotation]*echo.AnnotationValue{echo.SidecarInject: {Value: strconv.FormatBool(false)}},
		}},
	}

	all = echo.Instances{a1, a2, b1, b2, c1, c2, aNs1, aNs2, vm1, vm2, headless1, headless2, naked1, naked2, external1, external2}
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).EnvironmentFactory(resource.NilEnvironmentFactory).Run()
}

func TestIsRegularPod(t *testing.T) {
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
			if got := RegularPod(tt.app); got != tt.expect {
				t.Errorf("got %v expected %v", got, tt.expect)
			}
		})
	}
}

func TestIsNaked(t *testing.T) {
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

func TestDeployments(t *testing.T) {
	if diff := cmp.Diff(
		all.Services().Services(),
		[]string{"a", "a", "b", "c", "external", "headless", "naked", "vm"},
	); diff != "" {
		t.Fatal(diff)
	}
}

func TestFilters(t *testing.T) {
	tests := map[string]struct {
		filter func(echo.Instances) echo.Instances
		expect echo.Instances
	}{
		"SingleSimplePodServiceAndAllSpecial": {
			filter: SingleSimplePodServiceAndAllSpecial(),
			expect: echo.Instances{
				// keep all instances of this pod-based service
				a1, a2,
				// keep the special cases
				vm1, vm2,
				headless1, headless2,
				naked1, naked2,
				external1, external2,
			},
		},
		"ReachableDestinations from pod": {
			filter: func(instances echo.Instances) echo.Instances {
				return ReachableDestinations(a1, instances)
			},
			expect: echo.Instances{
				// all instances
				a1, a2, aNs1, aNs2, b1, b2, c1, c2, vm1, vm2, external1, external2,
				// only same network/cluster
				headless1, naked1,
			},
		},
		"ReachableDestinations from naked": {
			filter: func(instances echo.Instances) echo.Instances {
				return ReachableDestinations(naked1, instances)
			},
			expect: echo.Instances{
				// only same network/cluster, no VMs
				a1, aNs1, b1, c1, headless1, naked1, external1,
			},
		},
		"ReachableDestinations from vm": {
			filter: func(instances echo.Instances) echo.Instances {
				return ReachableDestinations(vm1, instances)
			},
			expect: echo.Instances{
				// all pods/vms, no external
				a1, a2, aNs1, aNs2, b1, b2, c1, c2, vm1, vm2,
				// only same network/cluster
				headless1, naked1,
			},
		},
	}
	for n, tc := range tests {
		n, tc := n, tc
		t.Run(n, func(t *testing.T) {
			compare(t, tc.filter(all), tc.expect)
		})
	}
}

func compare(t *testing.T, got echo.Instances, want echo.Instances) {
	if len(got) != len(want) {
		t.Errorf("got %d instnaces but expected %d", len(got), len(want))
	}
	expected := map[string]struct{}{}
	for _, i := range want {
		expected[instanceKey(i)] = struct{}{}
	}
	unexpected := map[string]struct{}{}
	for _, i := range all {
		k := instanceKey(i)
		if _, ok := expected[k]; !ok {
			unexpected[k] = struct{}{}
		}
	}
	for _, i := range got {
		k := instanceKey(i)
		// just remove the items rather than looping over expected, if anythings left we missed it
		delete(expected, k)
		if _, ok := unexpected[k]; ok {
			t.Errorf("expected %s to be filtered out", k)
		}
	}
	if len(expected) > 0 {
		t.Errorf("did not include %v", expected)
	}
}

func TestRun(t *testing.T) {
	// source svc/cluster -> dest services -> number of subtests (should == num clusters)
	framework.NewTest(t).Run(func(t framework.TestContext) {
		tests := map[string]struct {
			run    func(t framework.TestContext, testTopology map[string]map[string]int)
			expect map[string]map[string]int
		}{
			"Run_WithDefaultFilters": {
				run: func(t framework.TestContext, testTopology map[string]map[string]int) {
					New(t, all).
						WithDefaultFilters().
						Run(func(ctx framework.TestContext, from echo.Instance, to echo.Target) {
							// TODO if the destinations would change based on which cluster then add cluster to srCkey
							fromKey := from.Config().ClusterLocalFQDN()
							toKey := to.Config().ClusterLocalFQDN()
							if testTopology[fromKey] == nil {
								testTopology[fromKey] = map[string]int{}
							}
							testTopology[fromKey][toKey]++
						})
				},
				expect: map[string]map[string]int{
					"a.echo.svc.cluster.local": {
						"b.echo.svc.cluster.local":        2,
						"external.echo.svc.cluster.local": 2,
						"headless.echo.svc.cluster.local": 2,
						"naked.echo.svc.cluster.local":    2,
						"vm.echo.svc.cluster.local":       2,
					},
					"headless.echo.svc.cluster.local": {
						"b.echo.svc.cluster.local":        2,
						"external.echo.svc.cluster.local": 2,
						"headless.echo.svc.cluster.local": 2,
						"naked.echo.svc.cluster.local":    2,
						"vm.echo.svc.cluster.local":       2,
					},
					"naked.echo.svc.cluster.local": {
						"b.echo.svc.cluster.local":        2,
						"external.echo.svc.cluster.local": 2,
						"headless.echo.svc.cluster.local": 2,
						"naked.echo.svc.cluster.local":    2,
					},
					"vm.echo.svc.cluster.local": {
						"b.echo.svc.cluster.local":        2,
						"headless.echo.svc.cluster.local": 2,
						"naked.echo.svc.cluster.local":    2,
						"vm.echo.svc.cluster.local":       2,
					},
				},
			},
			"RunToN": {
				run: func(t framework.TestContext, testTopology map[string]map[string]int) {
					New(t, all).
						WithDefaultFilters().
						FromMatch(match.And(match.IsNotNaked, match.IsNotHeadless)).
						ToMatch(match.IsNotHeadless).
						RunToN(3, func(ctx framework.TestContext, from echo.Instance, dsts echo.Services) {
							srcKey := from.Config().ClusterLocalFQDN()
							if testTopology[srcKey] == nil {
								testTopology[srcKey] = map[string]int{}
							}
							var dstnames []string
							for _, dst := range dsts {
								dstnames = append(dstnames, dst.Config().ClusterLocalFQDN())
							}
							dstKey := strings.Join(dstnames, "_")
							testTopology[srcKey][dstKey]++
						})
				},
				expect: map[string]map[string]int{
					"a.echo.svc.cluster.local": {
						"b.echo.svc.cluster.local_external.echo.svc.cluster.local_naked.echo.svc.cluster.local":  2,
						"external.echo.svc.cluster.local_naked.echo.svc.cluster.local_vm.echo.svc.cluster.local": 2,
					},
					"vm.echo.svc.cluster.local": {
						// VM cannot hit external services (https://github.com/istio/istio/issues/27154)
						"b.echo.svc.cluster.local_naked.echo.svc.cluster.local_vm.echo.svc.cluster.local": 2,
					},
				},
			},
		}

		for name, tt := range tests {
			tt := tt
			t.NewSubTest(name).Run(func(t framework.TestContext) {
				testTopology := map[string]map[string]int{}
				tt.run(t, testTopology)
				if diff := cmp.Diff(testTopology, tt.expect); diff != "" {
					t.Errorf(diff)
				}
			})
		}
	})
}

var _ echo.Instance = fakeInstance{}

func instanceKey(i echo.Instance) string {
	return fmt.Sprintf("%s.%s.%s", i.Config().Service, i.Config().Namespace.Name(), i.Config().Cluster.Name())
}

// fakeInstance wraps echo.Config for test-framework internals tests where we don't actually make calls
type fakeInstance echo.Config

func (f fakeInstance) Instances() echo.Instances {
	return echo.Instances{f}
}

func (f fakeInstance) ID() resource.ID {
	panic("implement me")
}

func (f fakeInstance) NamespacedName() model.NamespacedName {
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

func (f fakeInstance) Call(echo.CallOptions) (echoClient.Responses, error) {
	panic("implement me")
}

func (f fakeInstance) CallOrFail(test.Failer, echo.CallOptions) echoClient.Responses {
	panic("implement me")
}

func (f fakeInstance) Restart() error {
	panic("implement me")
}
