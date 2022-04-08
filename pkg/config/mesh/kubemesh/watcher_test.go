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

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/testing/protocmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	namespace string = "istio-system"
	name      string = "istio"
	key       string = "MeshConfig"
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
	setup := func(t test.Failer) (corev1.ConfigMapInterface, mesh.Watcher) {
		client := kube.NewFakeClient()
		cms := client.Kube().CoreV1().ConfigMaps(namespace)
		stop := make(chan struct{})
		t.Cleanup(func() { close(stop) })
		w := NewConfigMapWatcher(client, namespace, name, key, true, stop)
		AddUserMeshConfig(client, w, namespace, key, extraCmName, stop)
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
		retry.UntilOrFail(t, func() bool { return w.Mesh().GetIngressClass() == "core" }, retry.Delay(time.Millisecond), retry.Timeout(time.Second))
	})
	t.Run("user first", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmUser, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return w.Mesh().GetIngressClass() == "core" }, retry.Delay(time.Millisecond), retry.Timeout(time.Second))
	})
	t.Run("only user", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmUser, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return w.Mesh().GetIngressClass() == "user" }, retry.Delay(time.Millisecond), retry.Timeout(time.Second))
	})
	t.Run("only core", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return w.Mesh().GetIngressClass() == "core" }, retry.Delay(time.Millisecond), retry.Timeout(time.Second))
	})
	t.Run("invalid user config", func(t *testing.T) {
		cms, w := setup(t)
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cms.Create(context.Background(), cmUserinvalid, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return w.Mesh().GetIngressClass() == "core" }, retry.Delay(time.Millisecond), retry.Timeout(time.Second))
	})
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
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	w := NewConfigMapWatcher(client, namespace, name, key, false, stop)

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
		{updated: badCM2, expect: m},
		{updated: badCM, expect: m},
		{updated: cm, expect: m},

		{deleted: cm, expect: mesh.DefaultMeshConfig()},
		{added: badCM, expect: mesh.DefaultMeshConfig()},
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

			retry.UntilOrFail(t, func() bool { return cmp.Equal(w.Mesh(), step.expect, protocmp.Transform()) })
			retry.UntilOrFail(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return cmp.Equal(newM, step.expect, protocmp.Transform())
			})
		})
	}
}
