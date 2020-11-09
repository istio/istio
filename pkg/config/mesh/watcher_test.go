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

package mesh_test

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/filewatcher"
)

func TestNewWatcherWithBadInputShouldFail(t *testing.T) {
	g := NewWithT(t)
	_, err := mesh.NewFileWatcher(filewatcher.NewWatcher(), "")
	g.Expect(err).ToNot(BeNil())
}

func TestWatcherShouldNotifyHandlers(t *testing.T) {
	g := NewWithT(t)

	path := newTempFile(t)
	defer removeSilent(path)

	m := mesh.DefaultMeshConfig()
	writeMessage(t, path, &m)

	w := newWatcher(t, path)
	g.Expect(w.Mesh()).To(Equal(&m))

	doneCh := make(chan struct{}, 1)

	var newM *meshconfig.MeshConfig
	w.AddMeshHandler(func() {
		newM = w.Mesh()
		close(doneCh)
	})

	// Change the file to trigger the update.
	m.IngressClass = "foo"
	writeMessage(t, path, &m)

	select {
	case <-doneCh:
		g.Expect(newM).To(Equal(&m))
		g.Expect(w.Mesh()).To(Equal(newM))
		break
	case <-time.After(time.Second * 5):
		t.Fatal("timed out waiting for update")
	}
}

func newWatcher(t testing.TB, filename string) mesh.Watcher {
	t.Helper()
	w, err := mesh.NewFileWatcher(filewatcher.NewWatcher(), filename)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func newTempFile(t testing.TB) string {
	t.Helper()
	f, err := ioutil.TempFile("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSilent(f)

	path, err := filepath.Abs(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeMessage(t testing.TB, path string, msg proto.Message) {
	t.Helper()
	yml, err := protomarshal.ToYAML(msg)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, yml)
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := ioutil.WriteFile(path, []byte(content), 0666); err != nil {
		t.Fatal(err)
	}
}

func closeSilent(c io.Closer) {
	_ = c.Close()
}

func removeSilent(path string) {
	_ = os.RemoveAll(path)
}

func BenchmarkGetMesh(b *testing.B) {
	b.StopTimer()

	path := newTempFile(b)
	defer removeSilent(path)

	m := mesh.DefaultMeshConfig()
	writeMessage(b, path, &m)

	w := newWatcher(b, path)

	b.StartTimer()

	handler := func(mc *meshconfig.MeshConfig) {
		// Do nothing
	}

	for i := 0; i < b.N; i++ {
		handler(w.Mesh())
	}
}

const (
	namespace string = "istio-system"
	name      string = "istio"
	key       string = "mesh"
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
	w := mesh.NewConfigMapWatcher(client, namespace, name, key)

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
