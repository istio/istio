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

package istio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// dumpTestCluster is a fake primary cluster. It has no kubeconfig file and no port forwarding, so
// only the dumps that go through the Kubernetes API produce files.
type dumpTestCluster struct {
	kube.CLIClient
	cluster.Topology
	// logDelay is how long a container log download takes.
	logDelay time.Duration
}

func (c *dumpTestCluster) String() string { return c.Topology.String() }

func (c *dumpTestCluster) PodLogs(ctx context.Context, name, namespace, container string, previous bool) (string, error) {
	time.Sleep(c.logDelay)
	return c.CLIClient.PodLogs(ctx, name, namespace, container, previous)
}

func (c *dumpTestCluster) NewPortForwarder(string, string, string, int, int) (kube.PortForwarder, error) {
	return nil, errors.New("no port forwarding in the test cluster")
}

// dumpTestContext implements the parts of resource.Context that Dump uses.
type dumpTestContext struct {
	resource.Context
	dir      string
	clusters cluster.Clusters
}

func (c *dumpTestContext) ID() string                    { return "test" }
func (c *dumpTestContext) Clusters() cluster.Clusters    { return c.clusters }
func (c *dumpTestContext) AllClusters() cluster.Clusters { return c.clusters }
func (c *dumpTestContext) Settings() *resource.Settings  { return &resource.Settings{} }
func (c *dumpTestContext) CreateTmpDirectory(prefix string) (string, error) {
	return os.MkdirTemp(c.dir, prefix)
}

func TestDumpWritesStateBeforeReturning(t *testing.T) {
	cases := []struct {
		name     string
		clusters int
		// failList is the resource whose listing fails in cluster failCluster, if set.
		failList    string
		failCluster int
		// want is the directory of the cluster whose state must be on disk when Dump returns.
		want string
	}{
		{name: "single cluster", clusters: 1, want: "primary-0"},
		{name: "pod listing fails on a later cluster", clusters: 2, failList: "pods", failCluster: 1, want: "primary-0"},
		{name: "pod listing fails on an earlier cluster", clusters: 2, failList: "pods", failCluster: 0, want: "primary-1"},
		{
			name: "mutating webhook listing fails on an earlier cluster", clusters: 2,
			failList: "mutatingwebhookconfigurations", failCluster: 0, want: "primary-1",
		},
		{
			name: "validating webhook listing fails on an earlier cluster", clusters: 2,
			failList: "validatingwebhookconfigurations", failCluster: 0, want: "primary-1",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			all := cluster.Map{}
			var clusters cluster.Clusters
			for n := range tt.clusters {
				name := fmt.Sprintf("cluster-%d", n)
				client := kube.NewFakeClient(
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "istiod", Namespace: "istio-system"},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "discovery"}}},
					},
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "coredns"}}},
					},
					&admissionv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "istio-sidecar-injector"}},
					&admissionv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "istio-validator"}},
				)
				if tt.failList != "" && tt.failCluster == n {
					client.Kube().(*fake.Clientset).PrependReactor("list", tt.failList,
						func(k8stesting.Action) (bool, runtime.Object, error) {
							return true, nil, errors.New("connection refused")
						})
				}
				c := &dumpTestCluster{
					CLIClient: client,
					Topology:  cluster.NewTopology(cluster.Config{Name: name, PrimaryClusterName: name, ConfigClusterName: name}, all),
					logDelay:  200 * time.Millisecond,
				}
				all[name] = c
				clusters = append(clusters, c)
			}
			ctx := &dumpTestContext{dir: t.TempDir(), clusters: clusters}
			i := &istioImpl{cfg: Config{SystemNamespace: "istio-system"}}

			i.Dump(ctx)

			// One file from each kind of dump that the fake cluster can serve.
			for _, name := range []string{
				"istiod_discovery.log",
				"coredns_coredns.log",
				"istio-sidecar-injector_mutatingwebhook.yaml",
				"istio-validator_validatingwebhook.yaml",
			} {
				files, err := filepath.Glob(filepath.Join(ctx.dir, "istio-state*", tt.want, name))
				if err != nil {
					t.Fatal(err)
				}
				if len(files) != 1 {
					t.Errorf("expected %s of %s to be written when Dump returns, found %v", name, tt.want, files)
				}
			}
		})
	}
}
