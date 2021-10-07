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
			t.NewSubTest("cluster local").Run(func(t framework.TestContext) {
				istio.PatchMeshConfig(t, i.Settings().SystemNamespace, destination.Clusters(), fmt.Sprintf(`
serviceSettings: 
- settings:
    clusterLocal: true
  hosts:
  - "%s"
`, apps.PodB[0].Config().FQDN()))
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
								echo.ExpectReachedClusters(cluster.Clusters{source.Config().Cluster}),
							),
						})
					})
				}
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

				t.Config().ApplyYAMLOrFail(t, ns, secret)
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

func TestRemoveCluster(t *testing.T) {
	framework.NewTest(t).
		RequiresMinClusters(2).
		RequiresLocalControlPlane().
		Run(func(t framework.TestContext) {
			primary := t.Clusters().Primaries()[0]
			remote := t.Clusters().Exclude(primary)[0]

			secretName := "istio-remote-secret-" + remote.Name()
			secretNamespace := istio.GetOrFail(t, t).Settings().SystemNamespace

			secret, err := primary.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			err = primary.CoreV1().Secrets(secretNamespace).Delete(context.TODO(), secretName, metav1.DeleteOptions{})
			t.Cleanup(func() {
				_, err := primary.CoreV1().Secrets(secretNamespace).Create(context.TODO(), secret, metav1.CreateOptions{})
				if err != nil {
					t.Log(err)
				}
			})
			if err != nil {
				t.Fatal(err)
			}

			apps.PodA.GetOrFail(t, echo.InCluster(primary)).CallWithRetryOrFail(t, echo.CallOptions{
				Target:    apps.PodB[0],
				Validator: echo.ExpectReachedClusters(t.Clusters().Exclude(remote)),
			})
		})
}
