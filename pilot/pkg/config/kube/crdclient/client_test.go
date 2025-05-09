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

	"go.uber.org/atomic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api/pkg/consts"

	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/api/networking/v1beta1"
	clientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1"
	apiistioioapinetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func makeClient(t *testing.T, schemas collection.Schemas, f kubetypes.DynamicObjectFilter) (model.ConfigStoreController, kube.CLIClient) {
	fake := kube.NewFakeClient()
	if f != nil {
		kube.SetObjectFilter(fake, f)
	}
	for _, s := range schemas.All() {
		var annotations map[string]string
		if s.Group() == gvk.KubernetesGateway.Group {
			annotations = map[string]string{
				consts.BundleVersionAnnotation: consts.BundleVersion,
			}
		}
		clienttest.MakeCRDWithAnnotations(t, fake, s.GroupVersionResource(), annotations)
	}
	stop := test.NewStop(t)
	config := New(fake, Option{})
	go config.Run(stop)
	fake.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, config.HasSynced)
	return config, fake
}

func createResource(t *testing.T, store model.ConfigStoreController, r resource.Schema, configMeta config.Meta) config.Spec {
	pb, err := r.NewInstance()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Create(config.Config{
		Meta: configMeta,
		Spec: pb,
	}); err != nil {
		t.Fatalf("Create => got %v", err)
	}

	return pb
}

// Ensure that the client can run without CRDs present
func TestClientNoCRDs(t *testing.T) {
	schema := collection.NewSchemasBuilder().MustAdd(collections.Sidecar).Build()
	store, _ := makeClient(t, schema, nil)
	retry.UntilOrFail(t, store.HasSynced, retry.Timeout(time.Second))
	r := collections.VirtualService
	configMeta := config.Meta{
		Name:             "name",
		Namespace:        "ns",
		GroupVersionKind: r.GroupVersionKind(),
	}
	createResource(t, store, r, configMeta)

	retry.UntilSuccessOrFail(t, func() error {
		l := store.List(r.GroupVersionKind(), configMeta.Namespace)
		if len(l) != 0 {
			return fmt.Errorf("expected no items returned for unknown CRD, got %v", l)
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))
	retry.UntilOrFail(t, func() bool {
		return store.Get(r.GroupVersionKind(), configMeta.Name, configMeta.Namespace) == nil
	}, retry.Message("expected no items returned for unknown CRD"), retry.Timeout(time.Second*5), retry.Converge(5))
}

// Ensure that the client can run without CRDs present, but then added later
func TestClientDelayedCRDs(t *testing.T) {
	// ns1 is allowed, ns2 is not
	f := kubetypes.NewStaticObjectFilter(func(obj interface{}) bool {
		// When an object is deleted, obj could be a DeletionFinalStateUnknown marker item.
		object := controllers.ExtractObject(obj)
		if object == nil {
			return false
		}
		ns := object.GetNamespace()
		return ns == "ns1"
	})
	schema := collection.NewSchemasBuilder().MustAdd(collections.Sidecar).Build()
	store, fake := makeClient(t, schema, f)
	retry.UntilOrFail(t, store.HasSynced, retry.Timeout(time.Second))
	r := collections.VirtualService

	// Create a virtual service
	configMeta1 := config.Meta{
		Name:             "name1",
		Namespace:        "ns1",
		GroupVersionKind: r.GroupVersionKind(),
	}
	createResource(t, store, r, configMeta1)

	configMeta2 := config.Meta{
		Name:             "name2",
		Namespace:        "ns2",
		GroupVersionKind: r.GroupVersionKind(),
	}
	createResource(t, store, r, configMeta2)

	retry.UntilSuccessOrFail(t, func() error {
		l := store.List(r.GroupVersionKind(), "")
		if len(l) != 0 {
			return fmt.Errorf("expected no items returned for unknown CRD")
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))

	clienttest.MakeCRD(t, fake, r.GroupVersionResource())

	retry.UntilSuccessOrFail(t, func() error {
		l := store.List(r.GroupVersionKind(), "")
		if len(l) != 1 {
			return fmt.Errorf("expected items returned")
		}
		if l[0].Name != configMeta1.Name {
			return fmt.Errorf("expected `name1` returned")
		}
		return nil
	}, retry.Timeout(time.Second*10), retry.Converge(5))
}

// CheckIstioConfigTypes validates that an empty store can do CRUD operators on all given types
func TestClient(t *testing.T) {
	store, _ := makeClient(t, collections.PilotGatewayAPI().Union(collections.Kube), nil)
	configName := "test"
	configNamespace := "test-ns"
	timeout := retry.Timeout(time.Millisecond * 200)
	for _, r := range collections.PilotGatewayAPI().All() {
		name := r.Kind()
		t.Run(name, func(t *testing.T) {
			configMeta := config.Meta{
				GroupVersionKind: r.GroupVersionKind(),
				Name:             configName,
			}
			if !r.IsClusterScoped() {
				configMeta.Namespace = configNamespace
			}
			pb := createResource(t, store, r, configMeta)

			// Kubernetes is eventually consistent, so we allow a short time to pass before we get
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.Meta, configMeta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			}, timeout)

			// Validate it shows up in List
			retry.UntilSuccessOrFail(t, func() error {
				cfgs := store.List(r.GroupVersionKind(), configMeta.Namespace)
				if len(cfgs) != 1 {
					return fmt.Errorf("expected 1 config, got %v", len(cfgs))
				}
				for _, cfg := range cfgs {
					if !reflect.DeepEqual(cfg.Meta, configMeta) {
						return fmt.Errorf("get(%v) => got %v", name, cfg)
					}
				}
				return nil
			}, timeout)

			// check we can update object metadata
			annotations := map[string]string{
				"foo": "bar",
			}
			configMeta.Annotations = annotations
			if _, err := store.Update(config.Config{
				Meta: configMeta,
				Spec: pb,
			}); err != nil {
				t.Errorf("Unexpected Error in Update -> %v", err)
			}
			if r.StatusKind() != "" {
				stat, err := r.Status()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.UpdateStatus(config.Config{
					Meta:   configMeta,
					Status: stat,
				}); err != nil {
					t.Errorf("Unexpected Error in Update -> %v", err)
				}
			}
			var cfg *config.Config
			// validate it is updated
			retry.UntilSuccessOrFail(t, func() error {
				cfg = store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.Meta, configMeta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			})

			// check we can patch items
			var patchedCfg config.Config
			if _, err := store.(*Client).Patch(*cfg, func(cfg config.Config) (config.Config, types.PatchType) {
				cfg.Annotations["fizz"] = "buzz"
				patchedCfg = cfg
				return cfg, types.JSONPatchType
			}); err != nil {
				t.Errorf("unexpected err in Patch: %v", err)
			}
			// validate it is updated
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.Meta, patchedCfg.Meta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			})

			// Check we can remove items
			if err := store.Delete(r.GroupVersionKind(), configName, configNamespace, nil); err != nil {
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

	t.Run("update status", func(t *testing.T) {
		r := collections.WorkloadGroup
		name := "name1"
		namespace := "bar"
		cfgMeta := config.Meta{
			GroupVersionKind: r.GroupVersionKind(),
			Name:             name,
		}
		if !r.IsClusterScoped() {
			cfgMeta.Namespace = namespace
		}
		pb := &v1alpha3.WorkloadGroup{Probe: &v1alpha3.ReadinessProbe{PeriodSeconds: 6}}
		if _, err := store.Create(config.Config{
			Meta: cfgMeta,
			Spec: config.Spec(pb),
		}); err != nil {
			t.Fatalf("Create bad: %v", err)
		}

		retry.UntilSuccessOrFail(t, func() error {
			cfg := store.Get(r.GroupVersionKind(), name, cfgMeta.Namespace)
			if cfg == nil {
				return fmt.Errorf("cfg shouldn't be nil :(")
			}
			if !reflect.DeepEqual(cfg.Meta, cfgMeta) {
				return fmt.Errorf("something is deeply wrong....., %v", cfg.Meta)
			}
			return nil
		})

		stat := &v1alpha1.IstioStatus{
			Conditions: []*v1alpha1.IstioCondition{
				{
					Type:    "Health",
					Message: "heath is badd",
				},
			},
		}

		if _, err := store.UpdateStatus(config.Config{
			Meta:   cfgMeta,
			Spec:   config.Spec(pb),
			Status: config.Status(stat),
		}); err != nil {
			t.Errorf("bad: %v", err)
		}

		retry.UntilSuccessOrFail(t, func() error {
			cfg := store.Get(r.GroupVersionKind(), name, cfgMeta.Namespace)
			if cfg == nil {
				return fmt.Errorf("cfg can't be nil")
			}
			if !reflect.DeepEqual(cfg.Status, stat) {
				return fmt.Errorf("status %v does not match %v", cfg.Status, stat)
			}
			return nil
		})
	})
}

// TestClientInitialSyncSkipsOtherRevisions tests that the initial sync skips objects from other
// revisions.
func TestClientInitialSyncSkipsOtherRevisions(t *testing.T) {
	fake := kube.NewFakeClient()
	for _, s := range collections.Istio.All() {
		clienttest.MakeCRD(t, fake, s.GroupVersionResource())
	}

	// Populate the client with some ServiceEntrys such that 1/3 are in the default revision and
	// 2/3 are in different revisions.
	labels := []map[string]string{
		nil,
		{"istio.io/rev": "canary"},
		{"istio.io/rev": "prod"},
	}
	var expectedNoRevision []config.Config
	var expectedCanary []config.Config
	var expectedProd []config.Config
	for i := 0; i < 9; i++ {
		selectedLabels := labels[i%len(labels)]
		obj := &clientnetworkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-service-entry-%d", i),
				Namespace: "test",
				Labels:    selectedLabels,
			},
			Spec: v1alpha3.ServiceEntry{},
		}

		clienttest.NewWriter[*clientnetworkingv1alpha3.ServiceEntry](t, fake).Create(obj)
		// canary revision should receive only global objects and objects with the canary revision
		if selectedLabels == nil || reflect.DeepEqual(selectedLabels, labels[1]) {
			expectedCanary = append(expectedCanary, TranslateObject(obj, gvk.ServiceEntry, ""))
		}
		// prod revision should receive only global objects and objects with the prod revision
		if selectedLabels == nil || reflect.DeepEqual(selectedLabels, labels[2]) {
			expectedProd = append(expectedProd, TranslateObject(obj, gvk.ServiceEntry, ""))
		}
		// no revision should receive all objects
		expectedNoRevision = append(expectedNoRevision, TranslateObject(obj, gvk.ServiceEntry, ""))
	}

	storeCases := map[string][]config.Config{
		"":       expectedNoRevision, // No revision specified, should receive all events.
		"canary": expectedCanary,     // Only SEs from the canary revision should be received.
		"prod":   expectedProd,       // Only SEs from the prod revision should be received.
	}
	for rev, expected := range storeCases {
		store := New(fake, Option{
			Revision: rev,
		})

		tt := assert.NewTracker[string](t)
		store.RegisterEventHandler(gvk.ServiceEntry, TrackerHandler(tt))
		stop := test.NewStop(t)
		fake.RunAndWait(stop)
		go store.Run(stop)

		kube.WaitForCacheSync("test", stop, store.HasSynced)
		tt.WaitUnordered(slices.Map(expected, func(c config.Config) string {
			return fmt.Sprintf("add/%v/%v", c.GroupVersionKind.Kind, krt.GetKey(c))
		})...)
	}
}

func TestClientSync(t *testing.T) {
	obj := &clientnetworkingv1alpha3.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service-entry",
			Namespace: "test",
		},
		Spec: v1alpha3.ServiceEntry{},
	}
	fake := kube.NewFakeClient()
	clienttest.NewWriter[*clientnetworkingv1alpha3.ServiceEntry](t, fake).Create(obj)
	for _, s := range collections.Pilot.All() {
		clienttest.MakeCRD(t, fake, s.GroupVersionResource())
	}
	stop := test.NewStop(t)
	c := New(fake, Option{})

	events := atomic.NewInt64(0)
	c.RegisterEventHandler(gvk.ServiceEntry, func(c config.Config, c2 config.Config, event model.Event) {
		events.Inc()
	})
	go c.Run(stop)
	fake.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, c.HasSynced)
	// This MUST have been called by the time HasSynced returns true
	assert.Equal(t, events.Load(), 1)
}

func TestAlternativeVersions(t *testing.T) {
	fake := kube.NewFakeClient()
	fake.RunAndWait(test.NewStop(t))
	vs := apiistioioapinetworkingv1beta1.VirtualService{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "oo"},
		Spec:       v1beta1.VirtualService{Hosts: []string{"hello"}},
		Status:     v1alpha1.IstioStatus{},
	}
	_, err := fake.Istio().NetworkingV1beta1().VirtualServices("test").Create(context.Background(), &vs, metav1.CreateOptions{})
	assert.NoError(t, err)
}

// TrackerHandler returns an object handler that records each event
func TrackerHandler(tracker *assert.Tracker[string]) func(o config.Config, n config.Config, e model.Event) {
	return func(o config.Config, n config.Config, e model.Event) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", e, n.GroupVersionKind.Kind, krt.GetKey(n)))
	}
}
