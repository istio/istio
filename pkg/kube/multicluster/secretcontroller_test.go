// Copyright Istio Authors.
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

package multicluster

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const secretNamespace string = "istio-system"

type clusterCredential struct {
	clusterID  string
	kubeconfig []byte
}

func makeSecret(namespace string, secret string, clusterConfigs ...clusterCredential) *v1.Secret {
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret,
			Namespace: namespace,
			Labels: map[string]string{
				MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{},
	}

	for _, config := range clusterConfigs {
		s.Data[config.clusterID] = config.kubeconfig
	}
	return s
}

func TestKubeConfigOverride(t *testing.T) {
	var (
		expectedQPS   = float32(100)
		expectedBurst = 200
	)
	fakeRestConfig := &rest.Config{}
	BuildClientsFromConfig = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		for _, override := range configOverrides {
			override(fakeRestConfig)
		}
		return kube.NewFakeClient(), nil
	}
	clientset := kube.NewFakeClient()
	stopCh := test.NewStop(t)
	c := NewController(clientset, secretNamespace, "", mesh.NewFixedWatcher(nil), func(cfg *rest.Config) {
		cfg.QPS = expectedQPS
		cfg.Burst = expectedBurst
	})
	clientset.RunAndWait(stopCh)
	_ = c.Run(stopCh)
	t.Run("sync timeout", func(t *testing.T) {
		retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
	})
	kube.WaitForCacheSync("test", stopCh, c.HasSynced)
	secret0 := makeSecret(secretNamespace, "s0",
		clusterCredential{"c0", []byte("kubeconfig0-0")})

	t.Run("test kube config override", func(t *testing.T) {
		g := NewWithT(t)
		_, err := clientset.Kube().CoreV1().Secrets(secret0.Namespace).Create(context.TODO(), secret0, metav1.CreateOptions{})
		g.Expect(err).Should(BeNil())

		g.Eventually(func() *Cluster {
			return c.cs.GetByID("c0")
		}, 10*time.Second).ShouldNot(BeNil())

		g.Expect(fakeRestConfig).Should(Equal(&rest.Config{
			QPS:   expectedQPS,
			Burst: expectedBurst,
		}))
	})
}

func TestSecretController(t *testing.T) {
	BuildClientsFromConfig = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		return kube.NewFakeClient(), nil
	}

	client := kube.NewFakeClient()

	var (
		secret0 = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-0")})
		secret0UpdateKubeconfigChanged = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")})
		secret0UpdateKubeconfigSame = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")})
		secret0AddCluster = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
		secret0DeleteCluster = secret0UpdateKubeconfigChanged // "c0-1" cluster deleted
		secret0ReAddCluster  = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
		secret0ReDeleteCluster = secret0UpdateKubeconfigChanged // "c0-1" cluster re-deleted
		secret1                = makeSecret(secretNamespace, "s1",
			clusterCredential{"c1", []byte("kubeconfig1-0")})
		otherNSSecret = makeSecret("some-other-namespace", "s2",
			clusterCredential{"c1", []byte("kubeconfig1-0")})
	)

	secret0UpdateKubeconfigSame.Annotations = map[string]string{"foo": "bar"}

	steps := []struct {
		name string
		// only set one of these per step. The others should be nil.
		add    *v1.Secret
		update *v1.Secret
		delete *v1.Secret

		want []testHandler
	}{
		{
			name: "Create secret s0 and add kubeconfig for cluster c0, which will add remote cluster c0",
			add:  secret0,
			want: []testHandler{{"config", 1}, {"c0", 2}},
		},
		{
			name:   "Update secret s0 and update the kubeconfig of cluster c0, which will update remote cluster c0",
			update: secret0UpdateKubeconfigChanged,
			want:   []testHandler{{"config", 1}, {"c0", 3}},
		},
		{
			name:   "Update secret s0 but keep the kubeconfig of cluster c0 unchanged, which will not update remote cluster c0",
			update: secret0UpdateKubeconfigSame,
			want:   []testHandler{{"config", 1}, {"c0", 3}},
		},
		{
			name: "Update secret s0 and add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will add remote cluster c0-1 but will not update remote cluster c0",
			update: secret0AddCluster,
			want:   []testHandler{{"config", 1}, {"c0", 3}, {"c0-1", 4}},
		},
		{
			name: "Update secret s0 and delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will delete remote cluster c0-1 but will not update remote cluster c0",
			update: secret0DeleteCluster,
			want:   []testHandler{{"config", 1}, {"c0", 3}},
		},
		{
			name: "Update secret s0 and re-add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will add remote cluster c0-1 but will not update remote cluster c0",
			update: secret0ReAddCluster,
			want:   []testHandler{{"config", 1}, {"c0", 3}, {"c0-1", 5}},
		},
		{
			name: "Update secret s0 and re-delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will delete remote cluster c0-1 but will not update remote cluster c0",
			update: secret0ReDeleteCluster,
			want:   []testHandler{{"config", 1}, {"c0", 3}},
		},
		{
			name: "Create secret s1 and add kubeconfig for cluster c1, which will add remote cluster c1",
			add:  secret1,
			want: []testHandler{{"config", 1}, {"c0", 3}, {"c1", 6}},
		},
		{
			name:   "Delete secret s0, which will delete remote cluster c0",
			delete: secret0,
			want:   []testHandler{{"config", 1}, {"c1", 6}},
		},
		{
			name:   "Delete secret s1, which will delete remote cluster c1",
			delete: secret1,
			want:   []testHandler{{"config", 1}},
		},
		{
			name: "Add secret from another namespace",
			add:  otherNSSecret,
			want: []testHandler{{"config", 1}},
		},
	}

	// Start the secret controller and sleep to allow secret process to start.
	stopCh := test.NewStop(t)
	c := NewController(client, secretNamespace, "config", mesh.NewFixedWatcher(nil))
	client.RunAndWait(stopCh)
	secrets := clienttest.NewWriter[*v1.Secret](t, client)
	iter := 0
	component := BuildMultiClusterComponent(c, func(cluster *Cluster, stop <-chan struct{}) testHandler {
		iter++
		return testHandler{ID: cluster.ID, Iter: iter}
	})
	client.RunAndWait(stopCh)
	_ = c.Run(stopCh)
	t.Run("sync timeout", func(t *testing.T) {
		retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
	})
	kube.WaitForCacheSync("test", stopCh, c.HasSynced)

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			switch {
			case step.add != nil:
				secrets.Create(step.add)
			case step.update != nil:
				secrets.Update(step.update)
			case step.delete != nil:
				secrets.Delete(step.delete.Name, step.delete.Namespace)
			}

			assert.EventuallyEqual(t, component.All, step.want)
		})
	}
}

type testHandler struct {
	ID   cluster.ID
	Iter int
}

func (h testHandler) Close() {
}
