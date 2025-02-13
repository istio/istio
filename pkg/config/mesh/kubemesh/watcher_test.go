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

	"go.uber.org/atomic"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	namespace string = "istio-system"
	name      string = "istio"
	key       string = MeshConfigKey
)

func makeConfigMapWithName(name, resourceVersion string, data map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: resourceVersion,
		},
		Data: data,
	}
}

func makeConfigMap(resourceVersion string, data map[string]string) *v1.ConfigMap {
	return makeConfigMapWithName(name, resourceVersion, data)
}

func TestExtraConfigmap(t *testing.T) {
	extraCmName := "extra"

	cmCore := makeConfigMap("1", map[string]string{
		key: "ingressClass: core",
	})
	cmUser := makeConfigMapWithName(extraCmName, "1", map[string]string{
		key: "ingressClass: user",
	})
	cmUserinvalid := makeConfigMapWithName(extraCmName, "1", map[string]string{
		key: "ingressClass: 1",
	})
	setup := func(t *testing.T) (corev1.ConfigMapInterface, mesh.Watcher) {
		client := kube.NewFakeClient()
		cms := client.Kube().CoreV1().ConfigMaps(namespace)
		opts := krttest.Options(t)
		primaryMeshConfig := NewConfigMapSource(client, namespace, name, MeshConfigKey, opts)
		userMeshConfig := NewConfigMapSource(client, namespace, extraCmName, MeshConfigKey, opts)
		col := meshwatcher.NewCollection(opts, userMeshConfig, primaryMeshConfig)
		col.AsCollection().WaitUntilSynced(opts.Stop())
		w := meshwatcher.ConfigAdapter(col)

		client.RunAndWait(opts.Stop())
		return cms, w
	}

	t.Run("core first", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cms.Create(context.Background(), cmUser, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		assertMeshConfig(t, w, "core")
	})
	t.Run("user first", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmUser, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		assertMeshConfig(t, w, "core")
	})
	t.Run("only user", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmUser, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		assertMeshConfig(t, w, "user")
	})
	t.Run("only core", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		assertMeshConfig(t, w, "core")
	})
	t.Run("invalid user config", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cms.Create(context.Background(), cmUserinvalid, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		assertMeshConfig(t, w, "core")
	})
	t.Run("many updates", func(t *testing.T) {
		cms, w := setup(t)
		rev := atomic.NewInt32(1)
		mkMap := func(m, d string) *v1.ConfigMap {
			mm := makeConfigMapWithName(m, "1", map[string]string{
				key: fmt.Sprintf(`ingressClass: "%s"`, d),
			})
			mm.ResourceVersion = fmt.Sprint(rev.Inc())
			return mm
		}
		if _, err := cms.Create(context.Background(), mkMap(extraCmName, "init"), metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cms.Create(context.Background(), mkMap(name, "init"), metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		assertMeshConfig(t, w, "init")
		errCh := make(chan error, 2)
		for i := 0; i < 100; i++ {
			t.Log("iter", i)
			write := fmt.Sprint(i)
			wg := sync.WaitGroup{}
			wg.Add(2)
			go func() {
				defer wg.Done()
				if _, err := cms.Update(context.Background(), mkMap(extraCmName, write), metav1.UpdateOptions{}); err != nil {
					errCh <- err
				}
			}()
			go func() {
				defer wg.Done()
				if _, err := cms.Update(context.Background(), mkMap(name, write), metav1.UpdateOptions{}); err != nil {
					errCh <- err
				}
			}()
			wg.Wait()
			assert.EventuallyEqual(t, func() string {
				return w.Mesh().GetIngressClass()
			}, write,
				retry.Delay(time.Millisecond),
				retry.Timeout(time.Second*5),
				retry.Message("write failed "+write),
			)
			select {
			case err := <-errCh:
				t.Fatal(err)
			default:
			}
		}
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	})
}

func assertMeshConfig(t *testing.T, w mesh.Watcher, v string) {
	t.Helper()
	assert.EventuallyEqual(t, func() string { return w.Mesh().GetIngressClass() }, v, retry.Delay(time.Millisecond), retry.Timeout(time.Second))
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
	badCM := makeConfigMap("3", map[string]string{
		key: "bad yaml",
	})

	client := kube.NewFakeClient()
	cms := client.Kube().CoreV1().ConfigMaps(namespace)
	opts := krttest.Options(t)
	primaryMeshConfig := NewConfigMapSource(client, namespace, name, MeshConfigKey, opts)
	col := meshwatcher.NewCollection(opts, primaryMeshConfig)
	col.AsCollection().WaitUntilSynced(opts.Stop())
	w := meshwatcher.ConfigAdapter(col)
	client.RunAndWait(opts.Stop())

	var mu sync.Mutex
	newM := mesh.DefaultMeshConfig()
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
		{expect: mesh.DefaultMeshConfig()},
		{added: cm, expect: m},

		// Handle misconfiguration errors.
		{updated: badCM, expect: m},
		{updated: cm, expect: m},
		{updated: badCM, expect: m},
		{updated: cm, expect: m},

		{deleted: cm, expect: mesh.DefaultMeshConfig()},
		{added: badCM, expect: mesh.DefaultMeshConfig()},
	}

	for i, step := range steps {
		t.Run(fmt.Sprintf("[%v]", i), func(t *testing.T) {
			switch {
			case step.added != nil:
				_, err := cms.Create(context.TODO(), step.added, metav1.CreateOptions{})
				assert.NoError(t, err)
			case step.updated != nil:
				_, err := cms.Update(context.TODO(), step.updated, metav1.UpdateOptions{})
				assert.NoError(t, err)
			case step.deleted != nil:
				assert.NoError(t, cms.Delete(context.TODO(), step.deleted.Name, metav1.DeleteOptions{}))
			}

			assert.EventuallyEqual(t, w.Mesh, step.expect)
			assert.EventuallyEqual(t, func() *meshconfig.MeshConfig {
				mu.Lock()
				defer mu.Unlock()
				return protomarshal.Clone(newM)
			}, step.expect)
		})
	}
}
