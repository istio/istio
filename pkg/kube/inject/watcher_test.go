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

package inject

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
)

const (
	namespace string = "istio-system"
	cmName    string = "istio-sidecar-injector"
	configKey string = "config"
	valuesKey string = "values"
)

func makeConfigMap(resourceVersion string, data map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            cmName,
			ResourceVersion: resourceVersion,
		},
		Data: data,
	}
}

func TestNewConfigMapWatcher(t *testing.T) {
	configYaml := "policy: enabled"
	c, err := UnmarshalConfig([]byte(configYaml))
	if err != nil {
		t.Fatal(err)
	}
	valuesConfig := "sidecarInjectorWebhook: {}"

	cm := makeConfigMap("1", map[string]string{
		configKey: configYaml,
		valuesKey: valuesConfig,
	})
	badCM := makeConfigMap("2", map[string]string{
		configKey: configYaml,
	})
	badCM2 := makeConfigMap("3", map[string]string{
		valuesKey: valuesConfig,
	})
	badCM3 := makeConfigMap("4", map[string]string{
		configKey: "bad yaml",
		valuesKey: valuesConfig,
	})

	var mu sync.Mutex
	var newConfig *Config
	var newValues string

	client := kube.NewFakeClient()
	w := NewConfigMapWatcher(client, namespace, cmName, configKey, valuesKey)
	w.SetHandler(func(config *Config, values string) error {
		mu.Lock()
		defer mu.Unlock()
		newConfig = config
		newValues = values
		return nil
	})
	stop := make(chan struct{})
	go w.Run(stop)
	controller := w.(*configMapWatcher).c
	cache.WaitForCacheSync(stop, controller.HasSynced)
	client.RunAndWait(stop)

	cms := client.Kube().CoreV1().ConfigMaps(namespace)
	steps := []struct {
		added   *v1.ConfigMap
		updated *v1.ConfigMap
		deleted *v1.ConfigMap
	}{
		{added: cm},

		// Handle misconfiguration errors.
		{updated: badCM},
		{updated: cm},
		{updated: badCM2},
		{updated: badCM},
		{updated: cm},
		{updated: badCM3},
		{updated: cm},

		// Handle deletion.
		{deleted: cm},
		{added: cm},
	}

	for i, step := range steps {
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

			g.Eventually(func() *Config {
				mu.Lock()
				defer mu.Unlock()
				return newConfig
			}, time.Second).Should(Equal(&c))
			g.Eventually(func() string {
				mu.Lock()
				defer mu.Unlock()
				return newValues
			}, time.Second).Should(Equal(valuesConfig))
		})
	}
}
