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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
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

func makeSecret(secret string, clusterConfigs ...clusterCredential) *v1.Secret {
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret,
			Namespace: secretNamespace,
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

func (h handler) ClusterAdded(cluster *Cluster, stop <-chan struct{}) error {
	mu.Lock()
	defer mu.Unlock()
	added = cluster.ID
	return nil
}

func (h handler) ClusterUpdated(cluster *Cluster, stop <-chan struct{}) error {
	mu.Lock()
	defer mu.Unlock()
	updated = cluster.ID
	return nil
}

func (h handler) ClusterDeleted(id cluster.ID) error {
	mu.Lock()
	defer mu.Unlock()
	deleted = id
	return nil
}

func resetCallbackData() {
	added = ""
	updated = ""
	deleted = ""
}

func Test_SecretController(t *testing.T) {
	BuildClientsFromConfig = func(kubeConfig []byte) (kube.Client, error) {
		return kube.NewFakeClient(), nil
	}
	test.SetDurationForTest(t, &features.RemoteClusterTimeout, 10*time.Nanosecond)
	clientset := kube.NewFakeClient()

	var (
		secret0                        = makeSecret("s0", clusterCredential{"c0", []byte("kubeconfig0-0")})
		secret0UpdateKubeconfigChanged = makeSecret("s0", clusterCredential{"c0", []byte("kubeconfig0-1")})
		secret0UpdateKubeconfigSame    = makeSecret("s0", clusterCredential{"c0", []byte("kubeconfig0-1")})
		secret0AddCluster              = makeSecret("s0", clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
		secret0DeleteCluster           = secret0UpdateKubeconfigChanged // "c0-1" cluster deleted
		secret1                        = makeSecret("s1", clusterCredential{"c1", []byte("kubeconfig1-0")})
	)
	secret0UpdateKubeconfigSame.Annotations = map[string]string{"foo": "bar"}

	steps := []struct {
		// only set one of these per step. The others should be nil.
		add    *v1.Secret
		update *v1.Secret
		delete *v1.Secret

		// only set one of these per step. The others should be empty.
		wantAdded   cluster.ID
		wantUpdated cluster.ID
		wantDeleted cluster.ID
	}{
		{add: secret0, wantAdded: "c0"},                             // 0
		{update: secret0UpdateKubeconfigChanged, wantUpdated: "c0"}, // 1
		{update: secret0UpdateKubeconfigSame},                       // 2
		{update: secret0AddCluster, wantAdded: "c0-1"},              // 3
		{update: secret0DeleteCluster, wantDeleted: "c0-1"},         // 4
		{add: secret1, wantAdded: "c1"},                             // 5
		{delete: secret0, wantDeleted: "c0"},                        // 6
		{delete: secret1, wantDeleted: "c1"},                        // 7
	}

	// Start the secret controller and sleep to allow secret process to start.
	stopCh := make(chan struct{})
	t.Cleanup(func() {
		close(stopCh)
	})
	c := NewController(clientset, secretNamespace, "")
	c.AddHandler(&handler{})
	_ = c.Run(stopCh)
	t.Run("sync timeout", func(t *testing.T) {
		retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
	})
	kube.WaitForCacheSync(stopCh, c.informer.HasSynced)
	clientset.RunAndWait(stopCh)

	for i, step := range steps {
		resetCallbackData()

		t.Run(fmt.Sprintf("[%v]", i), func(t *testing.T) {
			g := NewWithT(t)

			switch {
			case step.add != nil:
				_, err := clientset.CoreV1().Secrets(secretNamespace).Create(context.TODO(), step.add, metav1.CreateOptions{})
				g.Expect(err).Should(BeNil())
			case step.update != nil:
				_, err := clientset.CoreV1().Secrets(secretNamespace).Update(context.TODO(), step.update, metav1.UpdateOptions{})
				g.Expect(err).Should(BeNil())
			case step.delete != nil:
				g.Expect(clientset.CoreV1().Secrets(secretNamespace).Delete(context.TODO(), step.delete.Name, metav1.DeleteOptions{})).
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
		allowlist sets.Set
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
