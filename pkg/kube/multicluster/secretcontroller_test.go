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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
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

var (
	mu      sync.Mutex
	added   cluster.ID
	updated cluster.ID
	deleted cluster.ID
)

var _ ClusterHandler = &handler{}

type handler struct{}

func (h handler) ClusterAdded(cluster *Cluster, stop <-chan struct{}) {
	mu.Lock()
	defer mu.Unlock()
	added = cluster.ID
}

func (h handler) ClusterUpdated(cluster *Cluster, stop <-chan struct{}) {
	mu.Lock()
	defer mu.Unlock()
	updated = cluster.ID
}

func (h handler) ClusterUpdatedInNeed(cluster *Cluster) {
	// DO NOTHING
}

func (h handler) ClusterDeleted(id cluster.ID) {
	mu.Lock()
	defer mu.Unlock()
	deleted = id
}

func (h handler) HasSynced() bool {
	return true
}

func resetCallbackData() {
	added = ""
	updated = ""
	deleted = ""
}

func Test_SecretController(t *testing.T) {
	BuildClientsFromConfig = func(kubeConfig []byte, c cluster.ID) (kube.Client, error) {
		return kube.NewFakeClient(), nil
	}

	clientset := kube.NewFakeClient()

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

		// only set one of these per step. The others should be empty.
		wantAdded   cluster.ID
		wantUpdated cluster.ID
		wantDeleted cluster.ID
	}{
		{
			name:      "Create secret s0 and add kubeconfig for cluster c0, which will add remote cluster c0",
			add:       secret0,
			wantAdded: "c0",
		},
		{
			name:        "Update secret s0 and update the kubeconfig of cluster c0, which will update remote cluster c0",
			update:      secret0UpdateKubeconfigChanged,
			wantUpdated: "c0",
		},
		{
			name:   "Update secret s0 but keep the kubeconfig of cluster c0 unchanged, which will not update remote cluster c0",
			update: secret0UpdateKubeconfigSame,
		},
		{
			name: "Update secret s0 and add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will add remote cluster c0-1 but will not update remote cluster c0",
			update:    secret0AddCluster,
			wantAdded: "c0-1",
		},
		{
			name: "Update secret s0 and delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will delete remote cluster c0-1 but will not update remote cluster c0",
			update:      secret0DeleteCluster,
			wantDeleted: "c0-1",
		},
		{
			name: "Update secret s0 and re-add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will add remote cluster c0-1 but will not update remote cluster c0",
			update:    secret0ReAddCluster,
			wantAdded: "c0-1",
		},
		{
			name: "Update secret s0 and re-delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will delete remote cluster c0-1 but will not update remote cluster c0",
			update:      secret0ReDeleteCluster,
			wantDeleted: "c0-1",
		},
		{
			name:      "Create secret s1 and add kubeconfig for cluster c1, which will add remote cluster c1",
			add:       secret1,
			wantAdded: "c1",
		},
		{
			name:        "Delete secret s0, which will delete remote cluster c0",
			delete:      secret0,
			wantDeleted: "c0",
		},
		{
			name:        "Delete secret s1, which will delete remote cluster c1",
			delete:      secret1,
			wantDeleted: "c1",
		},
		{
			name: "Add secret from another namespace",
			add:  otherNSSecret,
		},
	}

	// Start the secret controller and sleep to allow secret process to start.
	stopCh := test.NewStop(t)
	c := NewController(clientset, secretNamespace, "", mesh.NewFixedWatcher(nil))
	clientset.RunAndWait(stopCh)
	c.AddHandler(&handler{})
	clientset.RunAndWait(stopCh)
	_ = c.Run(stopCh)
	t.Run("sync timeout", func(t *testing.T) {
		retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
	})
	kube.WaitForCacheSync("test", stopCh, c.HasSynced)

	for _, step := range steps {
		resetCallbackData()

		t.Run(step.name, func(t *testing.T) {
			g := NewWithT(t)

			switch {
			case step.add != nil:
				_, err := clientset.Kube().CoreV1().Secrets(step.add.Namespace).Create(context.TODO(), step.add, metav1.CreateOptions{})
				g.Expect(err).Should(BeNil())
			case step.update != nil:
				_, err := clientset.Kube().CoreV1().Secrets(step.update.Namespace).Update(context.TODO(), step.update, metav1.UpdateOptions{})
				g.Expect(err).Should(BeNil())
			case step.delete != nil:
				g.Expect(clientset.Kube().CoreV1().Secrets(step.delete.Namespace).Delete(context.TODO(), step.delete.Name, metav1.DeleteOptions{})).
					Should(Succeed())
			}

			switch {
			case step.wantAdded != "":
				g.Eventually(func() cluster.ID {
					mu.Lock()
					defer mu.Unlock()
					return added
				}, 10*time.Second).Should(Equal(step.wantAdded))
			case step.wantUpdated != "":
				g.Eventually(func() cluster.ID {
					mu.Lock()
					defer mu.Unlock()
					return updated
				}, 10*time.Second).Should(Equal(step.wantUpdated))
			case step.wantDeleted != "":
				g.Eventually(func() cluster.ID {
					mu.Lock()
					defer mu.Unlock()
					return deleted
				}, 10*time.Second).Should(Equal(step.wantDeleted))
			default:
				g.Consistently(func() bool {
					mu.Lock()
					defer mu.Unlock()
					return added == "" && updated == "" && deleted == ""
				}).Should(Equal(true))
			}
		})
	}
}

func TestSanitizeKubeConfig(t *testing.T) {
	cases := []struct {
		name      string
		config    api.Config
		allowlist sets.String
		want      api.Config
		wantErr   bool
	}{
		{
			name:    "empty",
			config:  api.Config{},
			want:    api.Config{},
			wantErr: false,
		},
		{
			name: "exec",
			config: api.Config{
				AuthInfos: map[string]*api.AuthInfo{
					"default": {
						Exec: &api.ExecConfig{
							Command: "sleep",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:      "exec allowlist",
			allowlist: sets.New("exec"),
			config: api.Config{
				AuthInfos: map[string]*api.AuthInfo{
					"default": {
						Exec: &api.ExecConfig{
							Command: "sleep",
						},
					},
				},
			},
			want: api.Config{
				AuthInfos: map[string]*api.AuthInfo{
					"default": {
						Exec: &api.ExecConfig{
							Command: "sleep",
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeKubeConfig(tt.config, tt.allowlist)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeKubeConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tt.config, tt.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
