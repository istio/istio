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

package secretcontroller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
)

const secretNamespace string = "istio-system"

func makeSecret(secret, clusterID string, kubeconfig []byte) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret,
			Namespace: secretNamespace,
			Labels: map[string]string{
				MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			clusterID: kubeconfig,
		},
	}
}

var (
	mu      sync.Mutex
	added   string
	updated string
	deleted string
)

func addCallback(_ kube.Client, id string) error {
	mu.Lock()
	defer mu.Unlock()
	added = id
	return nil
}

func updateCallback(_ kube.Client, id string) error {
	mu.Lock()
	defer mu.Unlock()
	updated = id
	return nil
}
func deleteCallback(id string) error {
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
	clientset := fake.NewSimpleClientset()

	var (
		secret0                        = makeSecret("s0", "c0", []byte("kubeconfig0-0"))
		secret0UpdateKubeconfigChanged = makeSecret("s0", "c0", []byte("kubeconfig0-1"))
		secret0UpdateKubeconfigSame    = makeSecret("s0", "c0", []byte("kubeconfig0-1"))
		secret1                        = makeSecret("s1", "c1", []byte("kubeconfig1-0"))
	)
	secret0UpdateKubeconfigSame.Annotations = map[string]string{"foo": "bar"}

	steps := []struct {
		// only set one of these per step. The others should be nil.
		add    *v1.Secret
		update *v1.Secret
		delete *v1.Secret

		// only set one of these per step. The others should be empty.
		wantAdded   string
		wantUpdated string
		wantDeleted string
	}{
		{add: secret0, wantAdded: "c0"},
		{update: secret0UpdateKubeconfigChanged, wantUpdated: "c0"},
		{update: secret0UpdateKubeconfigSame},
		{add: secret1, wantAdded: "c1"},
		{delete: secret0, wantDeleted: "c0"},
		{delete: secret1, wantDeleted: "c1"},
	}

	// Start the secret controller and sleep to allow secret process to start.
	stopCh := make(chan struct{})
	c := StartSecretController(clientset, addCallback, updateCallback, deleteCallback, secretNamespace)
	cache.WaitForCacheSync(stopCh, c.informer.HasSynced)

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
				g.Eventually(func() string {
					mu.Lock()
					defer mu.Unlock()
					return added
				}, 10*time.Second).Should(Equal(step.wantAdded))
			case step.wantUpdated != "":
				g.Eventually(func() string {
					mu.Lock()
					defer mu.Unlock()
					return updated
				}, 10*time.Second).Should(Equal(step.wantUpdated))
			case step.wantDeleted != "":
				g.Eventually(func() string {
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
