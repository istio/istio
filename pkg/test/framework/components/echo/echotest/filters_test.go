package echotest

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/cluster"

	"istio.io/istio/pkg/test/framework/components/echo"
)

var (
	// TODO set this up with echobuilder/cluster builder in Fake mode

	// 2 clusters on 2 networks
	cls1 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls1", Network: "n1"}}
	cls2 = &cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "cls2", Network: "n2"}}

	// simple pod
	a1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "a"}
	a2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "a"}
	// simple pod with different svc
	b1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "b"}
	b2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "b"}
	// another simple pod with different svc
	c1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "c"}
	c2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "c"}
	// simple pod with a different namespace
	aNs1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo2"), Service: "a"}
	aNs2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo2"), Service: "a"}
	// virtual machine
	vm1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "vm", DeployAsVM: true}
	vm2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "vm", DeployAsVM: true}
	// headless
	headless1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "headless", Headless: true}
	headless2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "headless", Headless: true}
	// naked pod (uninjected)
	naked1 = fakeInstance{Cluster: cls1, Namespace: fakeNamespace("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}
	naked2 = fakeInstance{Cluster: cls2, Namespace: fakeNamespace("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
		Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
	}}}

	all = echo.Instances{a1, a2, b1, b2, c1, c2, aNs1, aNs2, vm1, vm2, headless1, headless2, naked1, naked2}
)

func TestFilters(t *testing.T) {
	tests := map[string]struct {
		filter func(echo.Instances) echo.Instances
		expect echo.Instances
	}{
		"OneRegularPod": {
			filter: OnRegularPodSource,
			expect: echo.Instances{
				// keep all instances of this pod-based service
				a1, a2,
				// keep the special cases
				vm1, vm2,
				headless1, headless2,
				naked1, naked2,
			},
		},
		"ReachableDestinations from pod": {
			filter: func(instances echo.Instances) echo.Instances {
				return ReachableDestinations(a1, instances)
			},
			expect: echo.Instances{
				// all instances
				a1, a2, aNs1, aNs2, b1, b2, c1, c2, vm1, vm2,
				// only same network/cluster
				headless1, naked1,
			},
		},
		"ReachableDestinations from naked": {
			filter: func(instances echo.Instances) echo.Instances {
				return ReachableDestinations(naked1, instances)
			},
			expect: echo.Instances{
				// only same network/cluster
				a1, aNs1, b1, c1, vm1, headless1, naked1,
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
	expected := map[string]echo.Instance{}
	for _, i := range want {
		expected[instanceKey(i)] = i
	}
	unexpected := map[string]echo.Instance{}
	for _, i := range all {
		k := instanceKey(i)
		if _, ok := expected[k]; !ok {
			unexpected[k] = i
		}
	}
	for _, i := range got {
		k := instanceKey(i)
		if _, ok := expected[k]; !ok {
			t.Errorf("did not expect %s to be filtered out", k)
		}
		if _, ok := unexpected[k]; ok {
			t.Errorf("expected %s to be filtered out", k)
		}
	}
}
