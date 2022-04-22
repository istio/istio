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

package configmapwatcher

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
)

const (
	configMapNamespace string = "istio-system"
	configMapName      string = "watched"
)

func makeConfigMap(name, resourceVersion string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       configMapNamespace,
			Name:            name,
			ResourceVersion: resourceVersion,
		},
		Data: map[string]string{
			"mesh": "trustDomain: cluster.local",
		},
	}
}

var (
	mu     sync.Mutex
	called bool
	newCM  *v1.ConfigMap
)

func callback(cm *v1.ConfigMap) {
	mu.Lock()
	defer mu.Unlock()
	called = true
	newCM = cm
}

func getCalled() bool {
	mu.Lock()
	defer mu.Unlock()
	return called
}

func getCM() *v1.ConfigMap {
	mu.Lock()
	defer mu.Unlock()
	return newCM
}

func resetCalled() {
	called = false
	newCM = nil
}

func Test_ConfigMapWatcher(t *testing.T) {
	client := kube.NewFakeClient()
	cm := makeConfigMap(configMapName, "1")
	cm1 := makeConfigMap(configMapName, "2")
	cm2 := makeConfigMap("not-watched", "1")
	steps := []struct {
		added        *v1.ConfigMap
		updated      *v1.ConfigMap
		deleted      *v1.ConfigMap
		expectCalled bool
		expectCM     *v1.ConfigMap
	}{
		{added: cm2},
		{added: cm, expectCalled: true, expectCM: cm},
		{updated: cm},
		{updated: cm1, expectCalled: true, expectCM: cm1},
		{deleted: cm1, expectCalled: true},
		{deleted: cm2},
	}

	stop := make(chan struct{})
	c := NewController(client, configMapNamespace, configMapName, callback)
	go c.Run(stop)
	kube.WaitForCacheSync(stop, c.HasSynced)

	cms := client.Kube().CoreV1().ConfigMaps(configMapNamespace)
	for i, step := range steps {
		resetCalled()

		t.Run(fmt.Sprintf("[%v]", i), func(t *testing.T) {
			g := NewWithT(t)

			switch {
			case step.added != nil:
				_, err := cms.Create(context.TODO(), step.added, metav1.CreateOptions{})
				g.Expect(err).Should(BeNil())
			case step.updated != nil:
				_, err := cms.Update(context.TODO(), step.updated, metav1.UpdateOptions{})
				g.Expect(err).Should(BeNil())
			case step.deleted != nil:
				g.Expect(cms.Delete(context.TODO(), step.deleted.Name, metav1.DeleteOptions{})).
					Should(Succeed())
			}

			if step.expectCalled {
				g.Eventually(getCalled, time.Second).Should(Equal(true))
				g.Eventually(getCM, time.Second).Should(Equal(newCM))
			} else {
				g.Consistently(getCalled).Should(Equal(false))
			}
		})
	}
}
