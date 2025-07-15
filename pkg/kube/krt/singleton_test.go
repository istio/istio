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

package krt_test

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestSingleton(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	ConfigMaps := krt.NewInformer[*corev1.ConfigMap](c, opts.WithName("ConfigMaps")...)
	c.RunAndWait(stop)
	cmt := clienttest.NewWriter[*corev1.ConfigMap](t, c)
	meta := krt.Metadata{
		"app": "foo",
	}
	ConfigMapNames := krt.NewSingleton[string](
		func(ctx krt.HandlerContext) *string {
			cms := krt.Fetch(ctx, ConfigMaps)
			return ptr.Of(slices.Join(",", slices.Map(cms, func(c *corev1.ConfigMap) string {
				return config.NamespacedName(c).String()
			})...))
		}, opts.With(
			krt.WithName("ConfigMapNames"),
			krt.WithMetadata(meta),
		)...,
	)
	ConfigMapNames.AsCollection().WaitUntilSynced(stop)
	tt := assert.NewTracker[string](t)
	ConfigMapNames.Register(TrackerHandler[string](tt))
	tt.WaitOrdered("add/")

	assert.Equal(t, *ConfigMapNames.Get(), "")

	cmt.Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a",
			Namespace: "ns",
		},
	})
	tt.WaitUnordered("delete/", "add/ns/a")
	assert.Equal(t, *ConfigMapNames.Get(), "ns/a")
	assert.Equal(t, ConfigMapNames.AsCollection().Metadata(), meta)
}

func TestNewStatic(t *testing.T) {
	tt := assert.NewTracker[string](t)
	s := krt.NewStatic[string](nil, true)
	s.Register(TrackerHandler[string](tt))

	assert.Equal(t, s.Get(), nil)

	s.Set(ptr.Of("foo"))
	assert.Equal(t, s.Get(), ptr.Of("foo"))
	tt.WaitOrdered("add/foo")

	s.Set(nil)
	assert.Equal(t, s.Get(), nil)
	tt.WaitOrdered("delete/foo")

	s.Set(ptr.Of("bar"))
	assert.Equal(t, s.Get(), ptr.Of("bar"))
	tt.WaitOrdered("add/bar")

	s.Set(ptr.Of("bar2"))
	assert.Equal(t, s.Get(), ptr.Of("bar2"))
	tt.WaitOrdered("update/bar2")
}

func TestStaticMetadata(t *testing.T) {
	tt := assert.NewTracker[string](t)
	meta := krt.Metadata{
		"app": "foo",
	}
	s := krt.NewStatic[string](nil, true, krt.WithMetadata(meta))
	s.Register(TrackerHandler[string](tt))

	assert.Equal(t, s.Get(), nil)

	s.Set(ptr.Of("foo"))
	assert.Equal(t, s.Get(), ptr.Of("foo"))
	tt.WaitOrdered("add/foo")

	assert.Equal(t, s.AsCollection().Metadata(), meta)
}

// TrackerHandler returns an object handler that records each event
func TrackerHandler[T any](tracker *assert.Tracker[string]) func(krt.Event[T]) {
	return func(o krt.Event[T]) {
		tracker.Record(fmt.Sprintf("%v/%v", o.Event, krt.GetKey(o.Latest())))
	}
}

func BatchedTrackerHandler[T any](tracker *assert.Tracker[string]) func([]krt.Event[T]) {
	return func(o []krt.Event[T]) {
		tracker.Record(slices.Join(",", slices.Map(o, func(o krt.Event[T]) string {
			log.Infof("Received %s event for key %s in tracker", o.Event, krt.GetKey(o.Latest()))
			return fmt.Sprintf("%v/%v", o.Event, krt.GetKey(o.Latest()))
		})...))
	}
}
