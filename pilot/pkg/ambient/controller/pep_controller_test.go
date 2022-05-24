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

package controller

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	istiolog "istio.io/pkg/log"
)

func TestRemoteProxyController(t *testing.T) {
	vc, err := inject.NewValuesConfig(`
global:
  hub: test
  tag: test`)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := inject.ParseTemplates(map[string]string{"remote": file.AsStringOrFail(t, filepath.Join(env.IstioSrc, "manifests/charts/istio-control/istio-discovery/files/remote.yaml"))})
	if err != nil {
		t.Fatal(err)
	}
	run := func(name string, f func(t *testing.T, cc *RemoteProxyController)) {
		t.Run(name, func(t *testing.T) {
			c := kube.NewFakeClient(
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "test"}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-2", Namespace: "test"}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-3", Namespace: "cross-namespace"}},
			)

			cc := NewRemoteProxyController(c, "test", func() inject.WebhookConfig {
				return inject.WebhookConfig{
					Templates: tmpl,
					Values:    vc,
				}
			})
			stop := make(chan struct{})
			t.Cleanup(func() {
				close(stop)
				cc.queue.Run(stop)
			})
			c.RunAndWait(stop)
			remoteLog.SetOutputLevel(istiolog.DebugLevel)
			f(t, cc)
		})
	}

	run("empty", func(t *testing.T, cc *RemoteProxyController) {
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
	})

	run("full namespace gateway", func(t *testing.T, cc *RemoteProxyController) {
		_, err := cc.client.GatewayAPI().GatewayV1alpha2().Gateways("test").
			Create(context.Background(), makeGateway("gateway", ""), metav1.CreateOptions{})
		assert.NoError(t, err)
		time.Sleep(time.Millisecond * 100)
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
		assertCreated(t, cc.client,
			TypedNamed{Kind: gvk.Pod, Name: "sa-1-proxy", Namespace: "test"},
			TypedNamed{Kind: gvk.Pod, Name: "sa-2-proxy", Namespace: "test"},
		)

		assert.NoError(t, cc.client.GatewayAPI().GatewayV1alpha2().Gateways("test").
			Delete(context.Background(), "gateway", metav1.DeleteOptions{}))
		time.Sleep(time.Millisecond * 100)
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
		assertDeleted(t, cc.client,
			TypedNamed{Kind: gvk.Pod, Name: "sa-1-proxy", Namespace: "test"},
			TypedNamed{Kind: gvk.Pod, Name: "sa-2-proxy", Namespace: "test"},
		)
	})

	run("single SA gateway", func(t *testing.T, cc *RemoteProxyController) {
		_, err := cc.client.GatewayAPI().GatewayV1alpha2().Gateways("test").
			Create(context.Background(), makeGateway("gateway", "sa-1"), metav1.CreateOptions{})
		assert.NoError(t, err)
		time.Sleep(time.Millisecond * 100)
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
		assertCreated(t, cc.client,
			TypedNamed{Kind: gvk.Pod, Name: "sa-1-proxy", Namespace: "test"},
		)

		assert.NoError(t, cc.client.GatewayAPI().GatewayV1alpha2().Gateways("test").
			Delete(context.Background(), "gateway", metav1.DeleteOptions{}))
		time.Sleep(time.Millisecond * 100)
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
		assertDeleted(t, cc.client,
			TypedNamed{Kind: gvk.Pod, Name: "sa-1-proxy", Namespace: "test"},
		)
	})
}

type TypedNamed struct {
	Kind            config.GroupVersionKind
	Name, Namespace string
}

func assertCreated(t test.Failer, c kube.Client, expected ...TypedNamed) {
	t.Helper()
	have := []TypedNamed{}
	actions := c.Kube().(*fake.Clientset).Actions()
	for _, action := range actions {
		c, ok := action.(ktesting.CreateAction)
		if !ok {
			continue
		}
		o := c.GetObject().(metav1.Object)
		t := c.GetObject().GetObjectKind().GroupVersionKind()
		have = append(have, TypedNamed{
			Kind: config.GroupVersionKind{
				Group:   t.Group,
				Version: t.Version,
				Kind:    t.Kind,
			},
			Name:      o.GetName(),
			Namespace: o.GetNamespace(),
		})
	}
	sort.Slice(have, func(i, j int) bool {
		return have[i].Name < have[j].Name
	})
	sort.Slice(expected, func(i, j int) bool {
		return expected[i].Name < expected[j].Name
	})
	assert.Equal(t, expected, have)
}

func assertDeleted(t test.Failer, c kube.Client, expected ...TypedNamed) {
	t.Helper()
	have := []TypedNamed{}
	actions := c.Kube().(*fake.Clientset).Actions()
	for _, action := range actions {
		c, ok := action.(ktesting.DeleteAction)
		if !ok {
			continue
		}
		tt, f := collections.All.FindByGroupVersionResource(c.GetResource())
		if !f {
			t.Fatalf("unknown resource %v", c.GetResource())
		}
		have = append(have, TypedNamed{
			Kind: config.GroupVersionKind{
				Group:   tt.Resource().Group(),
				Version: tt.Resource().Version(),
				Kind:    tt.Resource().Kind(),
			},
			Name:      c.GetName(),
			Namespace: c.GetNamespace(),
		})
	}
	sort.Slice(have, func(i, j int) bool {
		return have[i].Name < have[j].Name
	})
	sort.Slice(expected, func(i, j int) bool {
		return expected[i].Name < expected[j].Name
	})
	assert.Equal(t, expected, have)
}

func makeGateway(s string, sa string) *v1alpha2.Gateway {
	gw := &v1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s,
			Namespace: "test",
		},
		Spec: v1alpha2.GatewaySpec{
			GatewayClassName: "istio-mesh",
		},
	}
	if sa != "" {
		gw.Annotations = map[string]string{
			"istio.io/service-account": sa,
		}
	}
	return gw
}
