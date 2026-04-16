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

	corev1 "k8s.io/api/core/v1"
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

type mountedKubeconfigFixture struct {
	sourceCluster cluster.Cluster
	targetCluster cluster.Cluster
	source        echo.Instance

	expectedClusters      cluster.Clusters
	expectedWithoutTarget cluster.Clusters
	originalData          map[string][]byte
}

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

// TestMountedRemoteKubeconfigUpdates verifies that removing one mounted kubeconfig
// file removes reachability to that remote cluster until the file is restored.
func TestMountedRemoteKubeconfigUpdates(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			fixture := newMountedKubeconfigFixture(t)

			fixture.updateSecretData(t, func(data map[string][]byte) {
				delete(data, fixture.targetKey())
			})
			assertReachabilityToClusters(t, fixture.source, apps.B, fixture.expectedWithoutTarget)

			fixture.restoreSecret(t)
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)
		})
}

// TestMountedRemoteKubeconfigDuplicateClusterIDKeepsExistingCluster verifies that
// adding a second mounted kubeconfig for the same cluster ID does not drop the
// existing remote cluster.
func TestMountedRemoteKubeconfigDuplicateClusterIDKeepsExistingCluster(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			fixture := newMountedKubeconfigFixture(t)

			fixture.updateSecretData(t, func(data map[string][]byte) {
				duplicateValue := append([]byte(nil), data[fixture.targetKey()]...)
				// Make the bytes differ so the kubeconfig hash changes and KRT keeps this
				// as a separate entry.
				duplicateValue = append(duplicateValue, []byte("\n# duplicate file with same cluster id\n")...)
				data["duplicate-"+fixture.targetKey()] = duplicateValue
			})

			// The duplicate file points at the same semantic cluster ID, so istiod
			// should keep using the existing remote cluster instead of dropping it.
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)

			// Restore the secret during the test so we can verify that removing the
			// duplicate kubeconfig does not affect cross cluster reachability.
			fixture.restoreSecret(t)
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)
		})
}

// TestMountedRemoteKubeconfigSameClusterIDUpdateKeepsExistingCluster verifies that
// rewriting one mounted kubeconfig for the same cluster ID preserves reachability.
func TestMountedRemoteKubeconfigSameClusterIDUpdateKeepsExistingCluster(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			fixture := newMountedKubeconfigFixture(t)

			fixture.updateSecretData(t, func(data map[string][]byte) {
				updatedValue := append([]byte(nil), data[fixture.targetKey()]...)
				updatedValue = append(updatedValue, []byte("\n# same cluster id, updated kubeconfig bytes\n")...)
				data[fixture.targetKey()] = updatedValue
			})

			// Updating the existing file for the same semantic cluster ID should
			// hot-swap the kubeconfig without dropping cross-cluster reachability.
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)
		})
}

// TestMountedRemoteKubeconfigMalformedContentKeepsExistingCluster verifies that a
// malformed mounted kubeconfig update leaves the last good remote cluster active.
func TestMountedRemoteKubeconfigMalformedContentKeepsExistingCluster(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			fixture := newMountedKubeconfigFixture(t)

			fixture.updateSecretData(t, func(data map[string][]byte) {
				data[fixture.targetKey()] = []byte("::not yaml::")
			})

			// A malformed reload should leave the last good kubeconfig snapshot active.
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)

			fixture.restoreSecret(t)
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)
		})
}

// TestMountedRemoteKubeconfigClusterIDChangeRemovesOldCluster verifies that
// rewriting one mounted kubeconfig from a remote cluster ID to the config-cluster
// ID removes the old remote cluster without adding a replacement, because
// remote-cluster creation ignores kubeconfigs that resolve to the config cluster.
func TestMountedRemoteKubeconfigClusterIDChangeRemovesOldCluster(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			fixture := newMountedKubeconfigFixture(t)

			localManifest, err := i.CreateRemoteSecret(t, fixture.sourceCluster)
			if err != nil {
				t.Fatal(err)
			}
			localKubeconfig, _, err := extractKubeconfig(localManifest)
			if err != nil {
				t.Fatal(err)
			}

			fixture.updateSecretData(t, func(data map[string][]byte) {
				data[fixture.targetKey()] = localKubeconfig
			})

			// Rewriting the file from the remote cluster ID to the local config-cluster
			// ID should remove the old remote cluster and not add a replacement.
			assertReachabilityToClusters(t, fixture.source, apps.B, fixture.expectedWithoutTarget)

			fixture.restoreSecret(t)
			assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)
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

func newMountedKubeconfigFixture(t framework.TestContext) mountedKubeconfigFixture {
	sourceCluster := t.Clusters().Primaries().Default()
	targetCluster := t.Clusters().Primaries().Exclude(sourceCluster).Default()
	expectedClusters := apps.B.Clusters()

	fixture := mountedKubeconfigFixture{
		sourceCluster:         sourceCluster,
		targetCluster:         targetCluster,
		source:                apps.A.ForCluster(sourceCluster.Name())[0],
		expectedClusters:      expectedClusters,
		expectedWithoutTarget: expectedClusters.Exclude(targetCluster),
	}

	assertCrossClusterReachability(t, fixture.source, apps.B, fixture.expectedClusters)

	fixture.originalData = cloneSecretData(fixture.getSecret(t).Data)
	t.Cleanup(func() {
		fixture.restoreSecret(t)
	})

	return fixture
}

func (f mountedKubeconfigFixture) getSecret(t framework.TestContext) *corev1.Secret {
	t.Helper()
	secret, err := f.sourceCluster.Kube().CoreV1().Secrets(systemNamespace).Get(context.Background(), remoteKubeconfigSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func (f mountedKubeconfigFixture) targetKey() string {
	return kubeconfigFileKey(f.targetCluster.Name())
}

func (f mountedKubeconfigFixture) updateSecretData(t framework.TestContext, mutate func(data map[string][]byte)) {
	t.Helper()
	secret := f.getSecret(t)
	data := cloneSecretData(secret.Data)
	mutate(data)
	secret.Data = data
	if _, err := f.sourceCluster.Kube().CoreV1().Secrets(systemNamespace).Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (f mountedKubeconfigFixture) restoreSecret(t framework.TestContext) {
	t.Helper()
	secret := f.getSecret(t)
	secret.Data = cloneSecretData(f.originalData)
	if _, err := f.sourceCluster.Kube().CoreV1().Secrets(systemNamespace).Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}
