//go:build integ

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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	multiclusterRetryTimeout = retry.Timeout(1 * time.Minute)
	multiclusterRetryDelay   = retry.Delay(500 * time.Millisecond)
)

func TestClusterLocal(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresMinClusters(2).
		RequireIstioVersion("1.11").
		Run(func(t framework.TestContext) {
			// TODO use echotest to dynamically pick 2 simple pods from apps.All
			sources := apps.A
			to := apps.B

			tests := []struct {
				name  string
				setup func(t framework.TestContext)
			}{
				{
					"MeshConfig.serviceSettings",
					func(t framework.TestContext) {
						i.PatchMeshConfigOrFail(t, fmt.Sprintf(`
serviceSettings: 
- settings:
    clusterLocal: true
  hosts:
  - "%s"
`, apps.B.Config().ClusterLocalFQDN()))
					},
				},
				{
					"subsets",
					func(t framework.TestContext) {
						cfg := tmpl.EvaluateOrFail(t, `
apiVersion: networking.istio.io/v1
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
apiVersion: networking.istio.io/v1
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
`, map[string]any{"src": sources, "dst": to, "host": to.Config().ClusterLocalFQDN()})
						t.ConfigIstio().YAML(sources.Config().Namespace.Name(), cfg).ApplyOrFail(t)
					},
				},
			}

			for _, test := range tests {
				t.NewSubTest(test.name).Run(func(t framework.TestContext) {
					test.setup(t)
					for _, source := range sources {
						t.NewSubTest(source.Config().Cluster.StableName()).RunParallel(func(t framework.TestContext) {
							source.CallOrFail(t, echo.CallOptions{
								To: to,
								Port: echo.Port{
									Name: "http",
								},
								Check: check.And(
									check.OK(),
									check.ReachedClusters(t.AllClusters(), cluster.Clusters{source.Config().Cluster}),
								),
								Retry: echo.Retry{
									Options: []retry.Option{multiclusterRetryDelay, multiclusterRetryTimeout},
								},
							})
						})
					}
				})
			}

			// this runs in a separate test context - confirms the cluster local config was cleaned up
			t.NewSubTest("cross cluster").Run(func(t framework.TestContext) {
				for _, source := range sources {
					t.NewSubTest(source.Config().Cluster.StableName()).Run(func(t framework.TestContext) {
						source.CallOrFail(t, echo.CallOptions{
							To: to,
							Port: echo.Port{
								Name: "http",
							},
							Check: check.And(
								check.OK(),
								check.ReachedTargetClusters(t),
							),
							Retry: echo.Retry{
								Options: []retry.Option{multiclusterRetryDelay, multiclusterRetryTimeout},
							},
						})
					})
				}
			})
		})
}

func TestBadRemoteSecret(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresMinClusters(2).
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
			if _, err := remote.Kube().CoreV1().ServiceAccounts(ns).Create(context.TODO(), &corev1.ServiceAccount{
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
					secret, err = i.CreateRemoteSecret(t, remote, opts...)
					return err
				}, retry.Timeout(15*time.Second))

				t.ConfigKube().YAML(ns, secret).ApplyOrFail(t)
			}
			// Test exec auth
			// CreateRemoteSecret can never generate this, so create it manually
			t.ConfigIstio().YAML(ns, `apiVersion: v1
kind: Secret
metadata:
  annotations:
    networking.istio.io/cluster: bad
  creationTimestamp: null
  labels:
    istio/multiCluster: "true"
  name: istio-remote-secret-bad
stringData:
  bad: |
    apiVersion: v1
    kind: Config
    clusters:
    - cluster:
        server: https://127.0.0.1
      name: bad
    contexts:
    - context:
        cluster: bad
        user: bad
      name: bad
    current-context: bad
    users:
    - name: bad
      user:
        exec:
          command: /bin/sh
          args: ["-c", "hello world!"]
---
`).ApplyOrFail(t)

			// create a new istiod pod using the template from the deployment, but not managed by the deployment
			t.Logf("creating pod %s/%s", ns, pod)
			deps, err := primary.Kube().AppsV1().
				Deployments(ns).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=istiod"})
			if err != nil {
				t.Fatal(err)
			}
			if len(deps.Items) == 0 {
				t.Skip("no deployments with label app=istiod")
			}
			pods := primary.Kube().CoreV1().Pods(ns)
			podMeta := deps.Items[0].Spec.Template.ObjectMeta
			podMeta.Name = pod
			template := deps.Items[0].Spec.Template.Spec
			for _, container := range template.Containers {
				if container.Name != "discovery" {
					continue
				}
				container.Env = append(container.Env, corev1.EnvVar{
					Name:  "PILOT_REMOTE_CLUSTER_TIMEOUT",
					Value: "15s",
				})
			}
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

			retry.UntilSuccessOrFail(t, func() error {
				pod, err := pods.Get(context.TODO(), pod, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, status := range pod.Status.ContainerStatuses {
					if status.Started != nil && !*status.Started {
						return fmt.Errorf("container %s in %s is not started", status.Name, pod)
					}
				}
				return nil
			}, retry.Timeout(5*time.Minute), retry.Delay(time.Second))

			// make sure the pod comes up healthy
			retry.UntilSuccessOrFail(t, func() error {
				pod, err := pods.Get(context.TODO(), pod, metav1.GetOptions{})
				if err != nil {
					return err
				}
				status := pod.Status.ContainerStatuses
				if len(status) < 1 || !status[0].Ready {
					conditions, _ := yaml.Marshal(pod.Status.ContainerStatuses)
					return fmt.Errorf("%s not ready: %s", pod.Name, conditions)
				}
				return nil
			}, retry.Timeout(time.Minute), retry.Delay(time.Second))
		})
}
