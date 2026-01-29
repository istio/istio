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

package remotekubeconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterdebug "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util"
)

func TestRemoteKubeconfigFromFile(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			if t.Clusters().Len() < 2 {
				t.Skip("requires at least 2 clusters")
			}
			primaries := t.Clusters().Primaries()
			if len(primaries) == 0 {
				t.Skip("no primary clusters available")
			}
			primary := primaries.Default()
			remotes := t.Clusters().Exclude(primary)
			if len(remotes) == 0 {
				t.Skip("no remote clusters available")
			}

			data := map[string][]byte{}
			for _, remote := range remotes {
				kubeconfigBytes, err := util.KubeconfigForCluster(remote)
				if err != nil {
					t.Fatalf("failed to build kubeconfig for %s: %v", remote.Name(), err)
				}
				filename := fmt.Sprintf("remote-%s.yaml", remote.Name())
				data[filename] = kubeconfigBytes
			}
			if err := upsertSecret(primary, i.Settings().SystemNamespace, data); err != nil {
				t.Fatalf("failed to update secret: %v", err)
			}

			retry.UntilOrFail(t, func() bool {
				infos, err := fetchClusterz(t, primary, i.Settings().SystemNamespace)
				if err != nil {
					t.Logf("clusterz fetch error: %v", err)
					return false
				}
				remaining := map[clusterdebug.ID]struct{}{}
				for _, remote := range remotes {
					remaining[clusterdebug.ID(remote.Name())] = struct{}{}
				}
				for _, info := range infos {
					delete(remaining, info.ID)
				}
				return len(remaining) == 0
			}, retry.Timeout(2*time.Minute), retry.Delay(2*time.Second))
		})
}

func fetchClusterz(t framework.TestContext, c cluster.Cluster, namespace string) ([]clusterdebug.DebugInfo, error) {
	istioctlClient, err := istioctl.New(t, istioctl.Config{Cluster: c, IstioNamespace: namespace})
	if err != nil {
		return nil, err
	}
	out, _, err := istioctlClient.Invoke([]string{"x", "internal-debug", "--all", "clusterz"})
	if err != nil {
		return nil, err
	}
	var perIstiod map[string]string
	if err := json.Unmarshal([]byte(out), &perIstiod); err != nil {
		return nil, err
	}
	var infos []clusterdebug.DebugInfo
	for _, raw := range perIstiod {
		var entry []clusterdebug.DebugInfo
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			return nil, err
		}
		infos = append(infos, entry...)
	}
	return infos, nil
}

func upsertSecret(c cluster.Cluster, namespace string, data map[string][]byte) error {
	client := c.Kube().CoreV1().Secrets(namespace)
	secret, err := client.Get(context.Background(), remoteKubeconfigSecret, metav1.GetOptions{})
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		_, err = client.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      remoteKubeconfigSecret,
				Namespace: namespace,
			},
			Data: data,
		}, metav1.CreateOptions{})
		return err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	for k, v := range data {
		secret.Data[k] = v
	}
	_, err = client.Update(context.Background(), secret, metav1.UpdateOptions{})
	return err
}
