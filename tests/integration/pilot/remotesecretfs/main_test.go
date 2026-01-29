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

package remotesecretfs

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	remoteKubeconfigSecret    = "istio-remote-kubeconfigs"
	remoteKubeconfigMountPath = "/var/run/remote-secrets"
)

var i istio.Instance

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		SkipIf("requires at least 2 clusters", func(ctx resource.Context) bool {
			return ctx.Clusters().Len() < 2
		}).
		Setup(func(t resource.Context) error {
			return ensureRemoteKubeconfigSecret(t, istio.DefaultSystemNamespace)
		}).
		Setup(istio.Setup(&i, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = fmt.Sprintf(`values:
  pilot:
    env:
      PILOT_REMOTE_CLUSTER_SECRET_PATH: "%s"
    volumeMounts:
    - name: remote-kubeconfigs
      mountPath: "%s"
    volumes:
    - name: remote-kubeconfigs
      secret:
        secretName: %s
`, remoteKubeconfigMountPath, remoteKubeconfigMountPath, remoteKubeconfigSecret)
		})).
		Run()
}

func ensureRemoteKubeconfigSecret(ctx resource.Context, namespace string) error {
	for _, c := range ctx.Clusters().Primaries() {
		if err := ensureNamespace(c, namespace); err != nil {
			return err
		}
		if err := upsertSecret(c, namespace, map[string][]byte{}); err != nil {
			return err
		}
	}
	return nil
}

func ensureNamespace(c cluster.Cluster, name string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_, err := c.Kube().CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
