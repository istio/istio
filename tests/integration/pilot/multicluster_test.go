//go:build integ
// +build integ

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

package pilot

import (
	"context"
	"fmt"
	"istio.io/istio/pkg/test/util/tmpl"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/util/retry"
)

func TestClusterLocal(t *testing.T) {
	framework.NewTest(t).
		Features(
			"installation.multicluster.cluster_local",
			// TODO tracking topologies as feature labels doesn't make sense
			"installation.multicluster.multimaster",
			"installation.multicluster.remote",
		).
		RequiresMinClusters(2).
		RequireIstioVersion("1.11").
		Run(func(t framework.TestContext) {
			// TODO use echotest to dynamically pick 2 simple pods from apps.All
			sources := apps.PodA
			destination := apps.PodB

			tests := []struct {
				name  string
				setup func(t framework.TestContext)
			}{
				{
					"MeshConfig.serviceSettings",
					func(t framework.TestContext) {
						istio.PatchMeshConfig(t, i.Settings().SystemNamespace, destination.Clusters(), fmt.Sprintf(`
serviceSettings: 
- settings:
    clusterLocal: true
  hosts:
  - "%s"
`, apps.PodB[0].Config().ClusterLocalFQDN()))
					},
				},
				{
					"subsets",
					func(t framework.TestContext) {
						cfg := tmpl.EvaluateOrFail(t, `
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: mysvc-dr
spec:
  host: {{.host}}
  subsets:
{{- range .dst }}
  - name: {{ .Config.Cluster.Name }}
    labels:
      topology.istio.io/cluster: {{ .Config.Cluster.Name }}
{{- end }}
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: mysvc-vs
spec:
  hosts:
  - {{.host}}
  http:
{{- range .dst }}
  - name: "{{ .Config.Cluster.Name }}-local"
    match:
    - sourceLabels:
        topology.istio.io/cluster: {{ .Config.Cluster.Name }}
    route:
    - destination:
        host: {{$.host}}
        subset: {{ .Config.Cluster.Name }}
{{- end }}
`, map[string]interface{}{"src": sources, "dst": destination, "host": destination[0].Config().ClusterLocalFQDN()})
						t.ConfigIstio().ApplyYAMLOrFail(t, sources[0].Config().Namespace.Name(), cfg)
					},
				},
			}

			for _, test := range tests {
				test := test
				t.NewSubTest(test.name).Run(func(t framework.TestContext) {
					test.setup(t)
					for _, source := range sources {
						source := source
						// TODO can probably RunParallel here
						t.NewSubTest(source.Config().Cluster.StableName()).Run(func(t framework.TestContext) {
							source.CallWithRetryOrFail(t, echo.CallOptions{
								Target:   destination[0],
								Count:    3 * len(destination),
								PortName: "http",
								Scheme:   scheme.HTTP,
								Validator: echo.And(
									echo.ExpectOK(),
									echo.ExpectReachedClusters(cluster.Clusters{source.Config().Cluster}),
								),
							})
						})
					}
				})
			}
			t.NewSubTest("MeshConfig.serviceSettings").Run(func(t framework.TestContext) {

			})
			t.NewSubTest("cross cluster").Run(func(t framework.TestContext) {
				// this runs in a separate test context - confirms the cluster local config was cleaned up
				for _, source := range sources {
					source := source
					t.NewSubTest(source.Config().Cluster.StableName()).Run(func(t framework.TestContext) {
						source.CallWithRetryOrFail(t, echo.CallOptions{
							Target:   destination[0],
							Count:    3 * len(destination),
							PortName: "http",
							Scheme:   scheme.HTTP,
							Validator: echo.And(
								echo.ExpectOK(),
								echo.ExpectReachedClusters(destination.Clusters()),
							),
						})
					})
				}
			})
		})
}

func TestBadRemoteSecret(t *testing.T) {
	framework.NewTest(t).
		RequiresMinClusters(2).
		Features(
			// TODO tracking topologies as feature labels doesn't make sense
			"installation.multicluster.multimaster",
			"installation.multicluster.remote",
		).
		Run(func(t framework.TestContext) {
			if len(t.Clusters().Primaries()) == 0 {
				t.Skip("no primary cluster in framework (most likely only remote-config)")
			}

			// we don't need to test this per-cluster
			primary := t.Clusters().Primaries()[0]
			// it doesn't matter if the other cluster is a primary/remote/etc.
			remote := t.Clusters().Exclude(primary)[0]

			var (
				ns  = i.Settings().SystemNamespace
				sa  = "istio-reader-no-perms"
				pod = "istiod-bad-secrets-test"
			)
			t.Logf("creating service account %s/%s", ns, sa)
			if _, err := remote.CoreV1().ServiceAccounts(ns).Create(context.TODO(), &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: sa},
			}, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}

			// intentionally not doing this with subtests since it would be pretty slow
			for _, opts := range [][]string{
				{"--name", "unreachable", "--server", "https://255.255.255.255"},
				{"--name", "no-permissions", "--service-account", "istio-reader-no-perms"},
			} {
				var secret string
				retry.UntilSuccessOrFail(t, func() error {
					var err error
					secret, err = istio.CreateRemoteSecret(t, remote, i.Settings(), opts...)
					return err
				}, retry.Timeout(15*time.Second))

				t.ConfigKube().ApplyYAMLOrFail(t, ns, secret)
			}

			// create a new istiod pod using the template from the deployment, but not managed by the deployment
			t.Logf("creating pod %s/%s", ns, pod)
			deps, err := primary.AppsV1().
				Deployments(ns).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=istiod"})
			if err != nil {
				t.Fatal(err)
			}
			if len(deps.Items) == 0 {
				t.Skip("no deployments with label app=istiod")
			}
			pods := primary.CoreV1().Pods(ns)
			podMeta := deps.Items[0].Spec.Template.ObjectMeta
			podMeta.Name = pod
			_, err = pods.Create(context.TODO(), &corev1.Pod{
				ObjectMeta: podMeta,
				Spec:       deps.Items[0].Spec.Template.Spec,
			}, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := pods.Delete(context.TODO(), pod, metav1.DeleteOptions{}); err != nil {
					t.Logf("error cleaning up %s: %v", pod, err)
				}
			})

			// make sure the pod comes up healthy
			retry.UntilSuccessOrFail(t, func() error {
				pod, err := pods.Get(context.TODO(), pod, metav1.GetOptions{})
				if err != nil {
					return err
				}
				status := pod.Status.ContainerStatuses
				if len(status) < 1 || !status[0].Ready {
					return fmt.Errorf("%s not ready", pod)
				}
				return nil
			}, retry.Timeout(time.Minute), retry.Delay(time.Second))
		})
}
