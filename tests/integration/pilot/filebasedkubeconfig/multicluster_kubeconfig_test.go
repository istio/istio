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

package filebasedkubeconfig

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	fileKubeconfigRetryTimeout = retry.Timeout(4 * time.Minute)
	fileKubeconfigRetryDelay   = retry.Delay(1 * time.Second)
)

func TestTrafficWithMountedRemoteKubeconfigs(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			assertNoLabeledRemoteSecrets(t)

			for _, source := range apps.A {
				t.NewSubTest(source.Config().Cluster.StableName()).RunParallel(func(t framework.TestContext) {
					assertCrossClusterReachability(t, source, apps.B, apps.B.Clusters())
				})
			}
		})
}

func TestMountedRemoteKubeconfigUpdates(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			sourceCluster := t.Clusters().Primaries().Default()
			targetCluster := t.Clusters().Primaries().Exclude(sourceCluster).Default()

			source := apps.A.ForCluster(sourceCluster.Name())[0]
			expectedClusters := apps.B.Clusters()
			expectedWithoutTarget := expectedClusters.Exclude(targetCluster)

			assertCrossClusterReachability(t, source, apps.B, expectedClusters)

			secretClient := sourceCluster.Kube().CoreV1().Secrets(systemNamespace)
			secret, err := secretClient.Get(context.Background(), remoteKubeconfigSecretName, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			originalData := cloneSecretData(secret.Data)

			t.Cleanup(func() {
				restore, err := secretClient.Get(context.Background(), remoteKubeconfigSecretName, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				restore.Data = cloneSecretData(originalData)
				if _, err := secretClient.Update(context.Background(), restore, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			})

			delete(secret.Data, kubeconfigFileKey(targetCluster.Name()))
			if _, err := secretClient.Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}

			assertReachabilityToClusters(t, source, apps.B, expectedWithoutTarget)

			restore, err := secretClient.Get(context.Background(), remoteKubeconfigSecretName, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			restore.Data = cloneSecretData(originalData)
			if _, err := secretClient.Update(context.Background(), restore, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}

			assertCrossClusterReachability(t, source, apps.B, expectedClusters)
		})
}

func assertNoLabeledRemoteSecrets(t framework.TestContext) {
	t.Helper()
	for _, c := range t.AllClusters().Primaries() {
		secrets, err := c.Kube().CoreV1().Secrets(systemNamespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: "istio/multiCluster=true",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(secrets.Items) > 0 {
			t.Fatalf("expected no labeled remote secrets on cluster %s, found %d", c.Name(), len(secrets.Items))
		}
	}
}

func assertCrossClusterReachability(t framework.TestContext, source echo.Instance, target echo.Instances, expected cluster.Clusters) {
	t.Helper()
	assertReachabilityToClusters(t, source, target, expected)
}

func assertReachabilityToClusters(t framework.TestContext, source echo.Instance, target echo.Instances, expected cluster.Clusters) {
	t.Helper()
	source.CallOrFail(t, echo.CallOptions{
		To: target,
		Port: echo.Port{
			Name: "http",
		},
		Count: requestCount(expected),
		Check: check.And(
			check.OK(),
			check.ReachedClusters(t.AllClusters(), expected),
		),
		Retry: echo.Retry{
			Options: []retry.Option{fileKubeconfigRetryDelay, fileKubeconfigRetryTimeout},
		},
	})
}

func requestCount(expected cluster.Clusters) int {
	if expected.Len()*10 > 20 {
		return expected.Len() * 10
	}
	return 20
}

func cloneSecretData(data map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(data))
	for key, value := range data {
		out[key] = append([]byte(nil), value...)
	}
	return out
}
