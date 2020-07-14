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

package crdclient

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/retry"
)

func makeClient(t *testing.T, schemas collection.Schemas) model.ConfigStoreCache {
	fake := kube.NewFakeClient()
	for _, s := range schemas.All() {
		fake.Ext().ApiextensionsV1beta1().CustomResourceDefinitions().Create(context.TODO(), &v1beta1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s.%s", s.Resource().Plural(), s.Resource().Group()),
			},
		}, metav1.CreateOptions{})
	}
	stop := make(chan struct{})
	config, err := New(fake, &model.DisabledLedger{}, "", controller.Options{})
	if err != nil {
		t.Fatal(err)
	}
	go config.Run(stop)
	fake.RunAndWait(stop)
	cache.WaitForCacheSync(stop, config.HasSynced)
	t.Cleanup(func() {
		close(stop)
	})
	return config
}

// Ensure that the client can run without CRDs present
func TestClientNoCRDs(t *testing.T) {
	schema := collection.NewSchemasBuilder().MustAdd(collections.IstioNetworkingV1Alpha3Sidecars).Build()
	store := makeClient(t, schema)
	retry.UntilSuccessOrFail(t, func() error {
		if !store.HasSynced() {
			return fmt.Errorf("store has not synced yet")
		}
		return nil
	}, retry.Timeout(time.Second))
	r := collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
	configMeta := model.ConfigMeta{
		Name:             "name",
		Namespace:        "ns",
		GroupVersionKind: r.GroupVersionKind(),
	}
	pb, err := r.NewProtoInstance()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Create(model.Config{
		ConfigMeta: configMeta,
		Spec:       pb,
	}); err != nil {
		t.Fatalf("Create => got %v", err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		l, err := store.List(r.GroupVersionKind(), configMeta.Namespace)
		// List should actually not return an error in this case; this allows running with missing CRDs
		// Instead, we just return an empty list.
		if err != nil {
			return fmt.Errorf("expected no error, but got %v", err)
		}
		if len(l) != 0 {
			return fmt.Errorf("expected no items returned for unknown CRD")
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))
	retry.UntilSuccessOrFail(t, func() error {
		l := store.Get(r.GroupVersionKind(), configMeta.Name, configMeta.Namespace)
		if l != nil {
			return fmt.Errorf("expected no items returned for unknown CRD, got %v", l)
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))
}

// CheckIstioConfigTypes validates that an empty store can do CRUD operators on all given types
func TestClient(t *testing.T) {
	store := makeClient(t, collections.PilotServiceApi)
	configName := "name"
	configNamespace := "namespace"
	timeout := retry.Timeout(time.Millisecond * 200)
	for _, c := range collections.PilotServiceApi.All() {
		name := c.Resource().Kind()
		t.Run(name, func(t *testing.T) {
			r := c.Resource()
			configMeta := model.ConfigMeta{
				GroupVersionKind: r.GroupVersionKind(),
				Name:             configName,
			}
			if !r.IsClusterScoped() {
				configMeta.Namespace = configNamespace
			}

			pb, err := r.NewProtoInstance()
			if err != nil {
				t.Fatal(err)
			}

			if _, err := store.Create(model.Config{
				ConfigMeta: configMeta,
				Spec:       pb,
			}); err != nil {
				t.Fatalf("Create(%v) => got %v", name, err)
			}
			// Kubernetes is eventually consistent, so we allow a short time to pass before we get
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.ConfigMeta, configMeta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			}, timeout)

			// Validate it shows up in List
			retry.UntilSuccessOrFail(t, func() error {
				cfgs, err := store.List(r.GroupVersionKind(), configNamespace)
				if err != nil {
					return err
				}
				if len(cfgs) != 1 {
					return fmt.Errorf("expected 1 config, got %v", len(cfgs))
				}
				for _, cfg := range cfgs {
					if !reflect.DeepEqual(cfg.ConfigMeta, configMeta) {
						return fmt.Errorf("get(%v) => got %v", name, cfg)
					}
				}
				return nil
			}, timeout)

			// Check we can remove items
			if err := store.Delete(r.GroupVersionKind(), configName, configNamespace); err != nil {
				t.Fatalf("failed to delete: %v", err)
			}
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configNamespace)
				if cfg != nil {
					return fmt.Errorf("get(%v) => got %v, expected item to be deleted", name, cfg)
				}
				return nil
			}, timeout)
		})
	}
}
