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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	topologylabel "istio.io/api/label"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/yml"
)

var (
	i    istio.Instance
	apps deployment.SingleNamespaceView
)

const (
	systemNamespace            = istio.DefaultSystemNamespace
	remoteKubeconfigSecretName = "istio-remote-kubeconfigs"
	remoteKubeconfigMountPath  = "/var/run/istio/remote-kubeconfigs"
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMultiPrimary().
		RequireMinClusters(2).
		Setup(istio.Setup(&i, setupConfigForMountedKubeconfigs, createEmptyMountedKubeconfigSecrets)).
		Setup(populateMountedKubeconfigSecrets).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Run()
}

func setupConfigForMountedKubeconfigs(_ resource.Context, cfg *istio.Config) {
	cfg.SkipDeployCrossClusterSecrets = true
	cfg.ControlPlaneValues = fmt.Sprintf(`
components:
  pilot:
    k8s:
      overlays:
      - kind: Deployment
        name: istiod
        patches:
        - path: spec.template.spec.volumes[-1]
          value: |-
            name: remote-kubeconfigs
            secret:
              secretName: %s
        - path: spec.template.spec.containers[name:discovery].volumeMounts[-1]
          value: |-
            name: remote-kubeconfigs
            mountPath: %s
            readOnly: true
values:
  pilot:
    env:
      PILOT_MULTICLUSTER_KUBECONFIG_PATH: %q
`, remoteKubeconfigSecretName, remoteKubeconfigMountPath, remoteKubeconfigMountPath)
}

func createEmptyMountedKubeconfigSecrets(ctx resource.Context) error {
	for _, c := range ctx.AllClusters().Primaries() {
		if err := ensureSystemNamespace(ctx, c); err != nil {
			return err
		}
		if err := upsertMountedKubeconfigSecret(c, map[string][]byte{}); err != nil {
			return err
		}
	}
	return nil
}

func ensureSystemNamespace(ctx resource.Context, c cluster.Cluster) error {
	labels := map[string]string{}
	if ctx.Environment().IsMultiNetwork() {
		labels[topologylabel.TopologyNetwork.Name] = c.NetworkName()
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   systemNamespace,
			Labels: labels,
		},
	}
	_, err := c.Kube().CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s on cluster %s: %w", systemNamespace, c.Name(), err)
	}
	return nil
}

func populateMountedKubeconfigSecrets(ctx resource.Context) error {
	for _, primary := range ctx.AllClusters().Primaries() {
		data := make(map[string][]byte)
		for _, remote := range ctx.Clusters().MeshClusters().Exclude(primary) {
			manifest, err := i.CreateRemoteSecret(ctx, remote)
			if err != nil {
				return fmt.Errorf("failed to create remote secret for cluster %s: %w", remote.Name(), err)
			}
			kubeconfig, clusterName, err := extractKubeconfig(manifest)
			if err != nil {
				return fmt.Errorf("failed to extract kubeconfig for cluster %s: %w", remote.Name(), err)
			}
			data[kubeconfigFileKey(clusterName)] = kubeconfig
		}
		if err := upsertMountedKubeconfigSecret(primary, data); err != nil {
			return err
		}
	}
	return nil
}

func extractKubeconfig(secretManifest string) ([]byte, string, error) {
	var generated corev1.Secret
	parts := yml.SplitString(secretManifest)
	if len(parts) == 0 {
		return nil, "", fmt.Errorf("empty remote secret manifest")
	}
	if err := yaml.Unmarshal([]byte(parts[0]), &generated); err != nil {
		return nil, "", err
	}
	if len(generated.StringData) == 1 {
		for clusterName, kubeconfig := range generated.StringData {
			return []byte(kubeconfig), clusterName, nil
		}
	}
	if len(generated.Data) == 1 {
		for clusterName, kubeconfig := range generated.Data {
			return kubeconfig, clusterName, nil
		}
	}
	return nil, "", fmt.Errorf("expected one kubeconfig entry, got stringData=%d data=%d", len(generated.StringData), len(generated.Data))
}

func upsertMountedKubeconfigSecret(c cluster.Cluster, data map[string][]byte) error {
	secrets := c.Kube().CoreV1().Secrets(systemNamespace)
	existing, err := secrets.Get(context.Background(), remoteKubeconfigSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      remoteKubeconfigSecretName,
				Namespace: systemNamespace,
			},
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create mounted kubeconfig secret on cluster %s: %w", c.Name(), err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get mounted kubeconfig secret on cluster %s: %w", c.Name(), err)
	}

	existing.Data = data
	if _, err := secrets.Update(context.Background(), existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update mounted kubeconfig secret on cluster %s: %w", c.Name(), err)
	}
	return nil
}

func kubeconfigFileKey(clusterName string) string {
	return clusterName + ".yaml"
}
