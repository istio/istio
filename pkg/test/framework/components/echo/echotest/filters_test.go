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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	// TODO set this up with echobuilder/cluster builder in Fake mode

	// 2 clusters on 2 networks
	cls1 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls1", Network: "n1", Index: 0, ClusterKind: cluster.Fake}}
	cls2 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls2", Network: "n2", Index: 1, ClusterKind: cluster.Fake}}

	// simple pod
	a1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "a"}
	a2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "a"}
	// simple pod with different svc
	b1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "b"}
	b2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "b"}
	// another simple pod with different svc
	c1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "c"}
	c2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "c"}
	// simple pod with a different namespace
	aNs1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo2"), Service: "a"}
	aNs2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo2"), Service: "a"}
	// virtual machine
	vm1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "vm", DeployAsVM: true}
	vm2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "vm", DeployAsVM: true}
	// headless
	headless1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "headless", Headless: true}
	headless2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "headless", Headless: true}
	// naked pod (uninjected)
	naked1 = &fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	naked2 = &fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	// external svc
	external1 = &fakeInstance{
		Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
			Annotations: map[echo.Annotation]*echo.AnnotationValue{echo.SidecarInject: {Value: strconv.FormatBool(false)}},
		}},
	}
	external2 = &fakeInstance{
		Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "external", DefaultHostHeader: "external.com", Subsets: []echo.SubsetConfig{{
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
			if got := isRegularPod(tt.app); got != tt.expect {
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

func TestDefaultFilters(t *testing.T) {
	// source svc/cluster -> dest services -> number of subtests (should == num clusters)
	testTopology := map[string]map[string]int{}
	framework.NewTest(t).Run(func(t framework.TestContext) {
		New(t, all).
			WithDefaultFilters().
			Run(func(ctx framework.TestContext, src echo.Instance, dst echo.Instances) {
				// TODO if the destinations would change based on which cluster then add cluster to srCkey
				srcKey := src.Config().FQDN()
				dstKey := dst[0].Config().FQDN()
				if testTopology[srcKey] == nil {
					testTopology[srcKey] = map[string]int{}
				}
				testTopology[srcKey][dstKey]++
			})
	})
	if diff := cmp.Diff(testTopology, map[string]map[string]int{
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
	}); diff != "" {
		t.Errorf(diff)
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
