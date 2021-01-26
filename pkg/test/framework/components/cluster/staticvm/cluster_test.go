package staticvm

import (
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/fake"
	"istio.io/istio/pkg/test/framework/components/echo"
	"testing"
)

func TestStaticCluster_Claim(t *testing.T) {
	all := make(cluster.Map)
	primary := &fake.Cluster{Topology: cluster.Topology{
		ClusterName:        "primary",
		PrimaryClusterName: "primary",
		AllClusters:        all,
	}}
	all["primary"] = primary
	otherPrimary := &fake.Cluster{Topology: cluster.Topology{
		ClusterName:        "otherPrimary",
		PrimaryClusterName: "otherPrimary",
		AllClusters:        all,
	}}
	all["otherPrimary"] = otherPrimary
	
	tests := map[string]struct {
		configs []echo.Config
		vms     []echo.Config
		claimed []echo.Config
		primary string
	}{
		"partial namespace match": {
			configs: []echo.Config{
				{Service: "notavm", Cluster: primary},
				{Service: "vm", Namespace: fakeNamespace("vmns-1234")},
			},
			vms:     []echo.Config{{Service: "vm", Namespace: fakeNamespace("vmns")}},
			claimed: []echo.Config{{Service: "vm", Namespace: fakeNamespace("vmns-1234")}},
		},
		"namespace mismatch": {
			configs: []echo.Config{
				{Service: "notavm", Cluster: primary},
				{Service: "vm", Namespace: fakeNamespace("wrongvmns-1234")},
			},
			vms: []echo.Config{{Service: "vm", Namespace: fakeNamespace("vmns")}},
		},
		"primary cluster mismatch": {
			configs: []echo.Config{
				{Service: "vm", Cluster: primary, Namespace: fakeNamespace("vmns")},
			},
			vms:     []echo.Config{{Service: "vm", Namespace: fakeNamespace("vmns")}},
			primary: "otherPrimary",
		},
		"primary cluster match": {
			configs: []echo.Config{
				{Service: "vm", Cluster: primary, Namespace: fakeNamespace("vmns")},
			},
			vms: []echo.Config{{Service: "vm", Namespace: fakeNamespace("vmns")}},
			claimed: []echo.Config{
				{Service: "vm", Cluster: primary, Namespace: fakeNamespace("vmns")},
			},
			primary: "primary",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.primary == "" {
				tc.primary = "vm"
			}
			vmCluster := &vmcluster{
				Topology: cluster.Topology{
					ClusterName:        "vm",
					PrimaryClusterName: tc.primary,
					AllClusters:        all,
				},
				vms: tc.vms,
			}
			all["vm"] = vmCluster

			claimed, unclaimed := vmCluster.Claim(tc.configs)
			if total := len(claimed) + len(unclaimed); total != len(tc.configs) {
				t.Errorf("%d configs were orignally given but %d were returned", len(tc.configs), total)
			}

			if len(claimed) != len(tc.claimed) {
				t.Fatalf("expected %d claimed configs but got %d", len(tc.claimed), len(claimed))
			}

			for i, config := range claimed {
				if !tc.claimed[i].Equals(config) {
					t.Fatal("claimed configs did not match")
				}
			}

		})
	}

}
