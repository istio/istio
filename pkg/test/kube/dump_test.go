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

package kube

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

type dumpTestCluster struct {
	kube.CLIClient
	cluster.Topology
}

func (c *dumpTestCluster) String() string { return c.Topology.String() }

// dumpTestContext implements the parts of resource.Context that DumpDeployments uses.
type dumpTestContext struct {
	resource.Context
	clusters cluster.Clusters
}

func (c *dumpTestContext) AllClusters() cluster.Clusters { return c.clusters }

func TestDumpDeploymentsListingFailure(t *testing.T) {
	cases := []struct {
		name        string
		failCluster int
		// want is the directory of the cluster whose deployment must be on disk when DumpDeployments returns.
		want string
	}{
		{name: "listing fails on a later cluster", failCluster: 1, want: "primary-0"},
		{name: "listing fails on an earlier cluster", failCluster: 0, want: "primary-1"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			all := cluster.Map{}
			var clusters cluster.Clusters
			for n := range 2 {
				name := fmt.Sprintf("cluster-%d", n)
				client := kube.NewFakeClient(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test"}})
				if tt.failCluster == n {
					client.Kube().(*fake.Clientset).PrependReactor("list", "deployments",
						func(k8stesting.Action) (bool, runtime.Object, error) {
							return true, nil, errors.New("connection refused")
						})
				}
				c := &dumpTestCluster{
					CLIClient: client,
					Topology:  cluster.NewTopology(cluster.Config{Name: name, PrimaryClusterName: name, ConfigClusterName: name}, all),
				}
				all[name] = c
				clusters = append(clusters, c)
			}
			dir := t.TempDir()

			DumpDeployments(&dumpTestContext{clusters: clusters}, dir, "test")

			if _, err := os.Stat(filepath.Join(dir, tt.want, "app_deployment.yaml")); err != nil {
				t.Fatalf("expected the deployment of %s to be written when DumpDeployments returns: %v", tt.want, err)
			}
		})
	}
}
