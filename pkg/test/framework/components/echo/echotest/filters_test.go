package echotest

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/cluster"

	"istio.io/istio/pkg/test/framework/components/echo"
)

func TestOneRegularPod(t *testing.T) {
	c1 := cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "c1"}}
	c2 := cluster.FakeCluster{Topology: cluster.Topology{ClusterName: "c2"}}
	want := echo.Instances{
		// keep this pod
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo"), Service: "a"},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo"), Service: "a"},
		// keep the special cases
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo"), Service: "vm", DeployAsVM: true},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo"), Service: "vm", DeployAsVM: true},
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo"), Service: "headless", Headless: true},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo"), Service: "headless", Headless: true},
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
			Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
		}}},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo"), Service: "naked", Subsets: []echo.SubsetConfig{{
			Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
		}}},
	}
	// filter these out
	filtered := echo.Instances{
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo2"), Service: "a"},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo2"), Service: "a"},
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo"), Service: "b"},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo"), Service: "b"},
		fakeInstance{Cluster: c1, Namespace: fakeNamespace("echo"), Service: "c"},
		fakeInstance{Cluster: c2, Namespace: fakeNamespace("echo"), Service: "c"},
	}

	out := OneRegularPod(append(want, filtered...))
	got := map[string]bool{}
	for _, i := range out {
		got[instanceKey(i)] = true
	}
	for _, i := range want {
		k := instanceKey(i)
		if !got[k] {
			t.Errorf("did not expect %s to be filtered out", k)
		}
	}
	for _, i := range filtered {
		k := instanceKey(i)
		if got[k] {
			t.Errorf("expected %s to be filtered out", k)
		}
	}
}
