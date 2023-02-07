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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	clientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

var testServiceEntry = &clientnetworkingv1alpha3.ServiceEntry{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-service-entry",
		Namespace: "test",
	},
	Spec: networkingv1alpha3.ServiceEntry{},
}

func TestObjectsFromOtherRevisionsSkipped(t *testing.T) {
	for _, tc := range []struct {
		name          string
		labels        map[string]string
		eventExpected bool
	}{
		{
			name:          "same revision",
			labels:        nil,
			eventExpected: true,
		},
		{
			name:          "different revision",
			labels:        map[string]string{"istio.io/rev": "canary"},
			eventExpected: false,
		},
	} {
		for _, event := range []model.Event{model.EventAdd, model.EventUpdate, model.EventDelete} {
			testName := event.String() + " " + tc.name
			t.Run(testName, func(t *testing.T) {
				schema := collections.IstioNetworkingV1Alpha3Serviceentries
				cfg := testServiceEntry.DeepCopy()
				cfg.Labels = tc.labels

				cl, h := buildClientAndCacheHandler(t, schema)

				// Register a handler that records all received events in a slice
				var events []model.Event
				cl.RegisterEventHandler(
					schema.Resource().GroupVersionKind(),
					func(old config.Config, cur config.Config, e model.Event) {
						events = append(events, e)
					},
				)

				err := h.onEvent(nil, cfg, event)
				assert.NoError(t, err)

				if tc.eventExpected {
					assert.Equal(t, []model.Event{event}, events)
				} else {
					assert.Equal(t, 0, len(events))
				}
			})
		}
	}
}

func TestUpdateInOtherRevision(t *testing.T) {
	for _, tc := range []struct {
		name           string
		oldCfg         func() runtime.Object
		deleteExpected bool
	}{
		{
			name: "no old version",
			oldCfg: func() runtime.Object {
				return nil
			},
			deleteExpected: false,
		},
		{
			name: "old version in different revision",
			oldCfg: func() runtime.Object {
				cfg := testServiceEntry.DeepCopy()
				cfg.Labels = map[string]string{"istio.io/rev": "staging"}
				return cfg
			},
			deleteExpected: false,
		},
		{
			// The old version was in our revision (default of ""), but has been moved to a
			// different revision. The event is expected to be change to a delete event.
			name: "old version in our revision",
			oldCfg: func() runtime.Object {
				return testServiceEntry.DeepCopy()
			},
			deleteExpected: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := collections.IstioNetworkingV1Alpha3Serviceentries
			cfg := testServiceEntry.DeepCopy()
			// Set a revision label that is not our current revision
			cfg.Labels = map[string]string{"istio.io/rev": "canary"}

			cl, h := buildClientAndCacheHandler(t, schema)

			// Register a handler that records all received events in a slice
			var events []model.Event
			cl.RegisterEventHandler(
				schema.Resource().GroupVersionKind(),
				func(old config.Config, cur config.Config, e model.Event) {
					events = append(events, e)
				},
			)

			err := h.onEvent(tc.oldCfg(), cfg, model.EventUpdate)
			assert.NoError(t, err)

			if tc.deleteExpected {
				assert.Equal(t, []model.Event{model.EventDelete}, events)
			} else {
				assert.Equal(t, 0, len(events))
			}
		})
	}
}

func buildClientAndCacheHandler(t test.Failer, schema collection.Schema) (*Client, *cacheHandler) {
	cl, err := New(kube.NewFakeClient(), Option{})
	assert.NoError(t, err)

	// The informer isn't actually used in the test, but a real one is needed for
	// constructing the cache handler, so just use an arbitrary one.
	informer, err := cl.client.KubeInformer().ForResource(corev1.SchemeGroupVersion.WithResource("pods"))
	assert.NoError(t, err)

	h := createCacheHandler(cl, schema, informer)

	return cl, h
}
