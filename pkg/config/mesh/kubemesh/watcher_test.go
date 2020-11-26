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

package kubemesh

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
)

const (
	namespace string = "istio-system"
	name      string = "istio"
	key       string = "MeshConfig"
)

func makeConfigMap(resourceVersion string, data map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: resourceVersion,
		},
		Data: data,
	}
}

func TestNewConfigMapWatcher(t *testing.T) {
	yaml := "trustDomain: something.new"
	m, err := mesh.ApplyMeshConfigDefaults(yaml)
	if err != nil {
		t.Fatal(err)
	}

	cm := makeConfigMap("1", map[string]string{
		key: yaml,
	})
	badCM := makeConfigMap("2", map[string]string{
		"other-key": yaml,
	})
	badCM2 := makeConfigMap("3", map[string]string{
		key: "bad yaml",
	})

	client := kube.NewFakeClient()
	cms := client.Kube().CoreV1().ConfigMaps(namespace)
	w := NewConfigMapWatcher(client, namespace, name, key)

	defaultMesh := mesh.DefaultMeshConfig()

	var mu sync.Mutex
	newM := &defaultMesh
	w.AddMeshHandler(func() {
		mu.Lock()
		defer mu.Unlock()
		newM = w.Mesh()
	})

	steps := []struct {
		added   *v1.ConfigMap
		updated *v1.ConfigMap
		deleted *v1.ConfigMap

		expect *meshconfig.MeshConfig
	}{
		{expect: &defaultMesh},
		{added: cm, expect: m},

		// Handle misconfiguration errors.
		{updated: badCM, expect: m},
		{updated: cm, expect: m},
		{updated: badCM2, expect: m},
		{updated: badCM, expect: m},
		{updated: cm, expect: m},

		{deleted: cm, expect: &defaultMesh},
		{added: badCM, expect: &defaultMesh},
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

			g.Eventually(w.Mesh).Should(Equal(step.expect))
			g.Eventually(func() *meshconfig.MeshConfig {
				mu.Lock()
				defer mu.Unlock()
				return newM
			}, time.Second).Should(Equal(step.expect))
		})
	}
}
