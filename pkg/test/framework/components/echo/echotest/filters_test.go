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

package echotest_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	// TODO set this up with echobuilder/cluster builder in Fake mode

	echo1NS = namespace.Static("echo1")
	echo2NS = namespace.Static("echo2")

	// 2 clusters on 2 networks
	cls1 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls1", Network: "n1", Index: 0, ClusterKind: cluster.Fake}}
	cls2 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls2", Network: "n2", Index: 1, ClusterKind: cluster.Fake}}

	// simple pod
	a1 = &fakeInstance{Cluster: cls1, Namespace: echo1NS, Service: "a"}
	a2 = &fakeInstance{Cluster: cls2, Namespace: echo1NS, Service: "a"}
	// simple pod with different svc
	b1 = &fakeInstance{Cluster: cls1, Namespace: echo1NS, Service: "b"}
	b2 = &fakeInstance{Cluster: cls2, Namespace: echo1NS, Service: "b"}
	// another simple pod with different svc
	c1 = &fakeInstance{Cluster: cls1, Namespace: echo1NS, Service: "c"}
	c2 = &fakeInstance{Cluster: cls2, Namespace: echo1NS, Service: "c"}
	// simple pod with a different namespace
	a1Ns2 = &fakeInstance{Cluster: cls1, Namespace: echo2NS, Service: "a"}
	a2Ns2 = &fakeInstance{Cluster: cls2, Namespace: echo2NS, Service: "a"}
	// virtual machine
	vm1 = &fakeInstance{Cluster: cls1, Namespace: echo1NS, Service: "vm", DeployAsVM: true}
	vm2 = &fakeInstance{Cluster: cls2, Namespace: echo1NS, Service: "vm", DeployAsVM: true}
	// headless
	headless1 = &fakeInstance{Cluster: cls1, Namespace: echo1NS, Service: "headless", Headless: true}
	headless2 = &fakeInstance{Cluster: cls2, Namespace: echo1NS, Service: "headless", Headless: true}
	// naked pod (uninjected)
	naked1 = &fakeInstance{Cluster: cls1, Namespace: echo1NS, Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	naked2 = &fakeInstance{Cluster: cls2, Namespace: echo1NS, Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	// external svc
	external1 = &fakeInstance{
		Cluster: cls1, Namespace: echo1NS, Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
			Annotations: map[echo.Annotation]*echo.AnnotationValue{echo.SidecarInject: {Value: strconv.FormatBool(false)}},
		}},
	}
	external2 = &fakeInstance{
		Cluster: cls2, Namespace: echo1NS, Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
			Annotations: map[echo.Annotation]*echo.AnnotationValue{echo.SidecarInject: {Value: strconv.FormatBool(false)}},
		}},
	}

	all = echo.Instances{a1, a2, b1, b2, c1, c2, a1Ns2, a2Ns2, vm1, vm2, headless1, headless2, naked1, naked2, external1, external2}
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).EnvironmentFactory(resource.NilEnvironmentFactory).Run()
}

func TestDeployments(t *testing.T) {
	if diff := cmp.Diff(
		all.Services().NamespacedNames(),
		echo.NamespacedNames{
			{
				Name:      "a",
				Namespace: echo1NS,
			},
			{
				Name:      "a",
				Namespace: echo2NS,
			},
			{
				Name:      "b",
				Namespace: echo1NS,
			},
			{
				Name:      "c",
				Namespace: echo1NS,
			},
			{
				Name:      "external",
				Namespace: echo1NS,
			},
			{
				Name:      "headless",
				Namespace: echo1NS,
			},
			{
				Name:      "naked",
				Namespace: echo1NS,
			},
			{
				Name:      "vm",
				Namespace: echo1NS,
			},
		},
	); diff != "" {
		t.Fatal(diff)
	}
}

func TestFilters(t *testing.T) {
	tests := map[string]struct {
		filter func(echo.Instances) echo.Instances
		expect echo.Instances
	}{
		"SimplePodServiceAndAllSpecial": {
			filter: echotest.SingleSimplePodServiceAndAllSpecial(),
			expect: echo.Instances{
				// Keep pods for one regular service per namespace.
				a1, a2, a1Ns2, a2Ns2,
				// keep the special cases
				vm1, vm2,
				headless1, headless2,
				naked1, naked2,
				external1, external2,
			},
		},
		"ReachableDestinations from pod": {
			filter: func(instances echo.Instances) echo.Instances {
				return echotest.ReachableDestinations(a1, instances)
			},
			expect: echo.Instances{
				// all instances
				a1, a2, a1Ns2, a2Ns2, b1, b2, c1, c2, vm1, vm2, external1, external2,
				// only same network/cluster
				headless1, naked1,
			},
		},
		"ReachableDestinations from naked": {
			filter: func(instances echo.Instances) echo.Instances {
				return echotest.ReachableDestinations(naked1, instances)
			},
			expect: echo.Instances{
				// only same network/cluster, no VMs
				a1, a1Ns2, b1, c1, headless1, naked1, external1,
			},
		},
		"ReachableDestinations from vm": {
			filter: func(instances echo.Instances) echo.Instances {
				return echotest.ReachableDestinations(vm1, instances)
			},
			expect: echo.Instances{
				// all pods/vms, no external
				a1, a2, a1Ns2, a2Ns2, b1, b2, c1, c2, vm1, vm2,
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
		t.Errorf("got %d instances but expected %d", len(got), len(want))
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
					echotest.New(t, all).
						WithDefaultFilters(1, 1).
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
					"a.echo1.svc.cluster.local": {
						"b.echo1.svc.cluster.local":        2,
						"external.echo1.svc.cluster.local": 2,
						"headless.echo1.svc.cluster.local": 2,
						"naked.echo1.svc.cluster.local":    2,
						"vm.echo1.svc.cluster.local":       2,
					},
					"a.echo2.svc.cluster.local": {
						"b.echo1.svc.cluster.local":        2,
						"external.echo1.svc.cluster.local": 2,
						"headless.echo1.svc.cluster.local": 2,
						"naked.echo1.svc.cluster.local":    2,
						"vm.echo1.svc.cluster.local":       2,
					},
					"headless.echo1.svc.cluster.local": {
						"b.echo1.svc.cluster.local":        2,
						"external.echo1.svc.cluster.local": 2,
						"headless.echo1.svc.cluster.local": 2,
						"naked.echo1.svc.cluster.local":    2,
						"vm.echo1.svc.cluster.local":       2,
					},
					"naked.echo1.svc.cluster.local": {
						"b.echo1.svc.cluster.local":        2,
						"external.echo1.svc.cluster.local": 2,
						"headless.echo1.svc.cluster.local": 2,
						"naked.echo1.svc.cluster.local":    2,
					},
					"vm.echo1.svc.cluster.local": {
						"b.echo1.svc.cluster.local":        2,
						"headless.echo1.svc.cluster.local": 2,
						"naked.echo1.svc.cluster.local":    2,
						"vm.echo1.svc.cluster.local":       2,
					},
				},
			},
			"RunToN": {
				run: func(t framework.TestContext, testTopology map[string]map[string]int) {
					echotest.New(t, all).
						WithDefaultFilters(1, 1).
						FromMatch(match.And(match.NotNaked, match.NotHeadless)).
						ToMatch(match.NotHeadless).
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
					"a.echo1.svc.cluster.local": {
						"b.echo1.svc.cluster.local_external.echo1.svc.cluster.local_naked.echo1.svc.cluster.local":  2,
						"external.echo1.svc.cluster.local_naked.echo1.svc.cluster.local_vm.echo1.svc.cluster.local": 2,
					},
					"a.echo2.svc.cluster.local": {
						"b.echo1.svc.cluster.local_external.echo1.svc.cluster.local_naked.echo1.svc.cluster.local":  2,
						"external.echo1.svc.cluster.local_naked.echo1.svc.cluster.local_vm.echo1.svc.cluster.local": 2,
					},
					"vm.echo1.svc.cluster.local": {
						// VM cannot hit external services (https://github.com/istio/istio/issues/27154)
						"b.echo1.svc.cluster.local_naked.echo1.svc.cluster.local_vm.echo1.svc.cluster.local": 2,
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

func (f fakeInstance) WithWorkloads(wl ...echo.Workload) echo.Instance {
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

func (f fakeInstance) ServiceName() string {
	return f.Config().Service
}

func (f fakeInstance) NamespaceName() string {
	return f.Config().NamespaceName()
}

func (f fakeInstance) ServiceAccountName() string {
	return f.Config().ServiceAccountName()
}

func (f fakeInstance) ClusterLocalFQDN() string {
	return f.Config().ClusterLocalFQDN()
}

func (f fakeInstance) ClusterSetLocalFQDN() string {
	return f.Config().ClusterSetLocalFQDN()
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

func (f fakeInstance) UpdateWorkloadLabel(add map[string]string, remove []string) error {
	panic("implement me")
}

func (f fakeInstance) Restart() error {
	panic("implement me")
}
