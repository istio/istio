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

package gateway

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestStatusCollections(t *testing.T) {
	stop := test.NewStop(t)
	fetch := func(q *TestStatusQueue) []string {
		return slices.Sort(slices.Map(q.Statuses(), func(e any) string {
			return *(e.(*string))
		}))
	}

	type Status = krt.ObjectWithStatus[*v1.ConfigMap, string]
	c := setupControllerWithoutGatewayClasses(t)
	obj1 := Status{
		Obj:    &v1.ConfigMap{},
		Status: "hello world",
	}
	fakeCol := krt.NewStaticCollection[Status](nil, []Status{obj1}, krt.WithStop(stop))
	status.RegisterStatus(c.status, fakeCol, func(i *v1.ConfigMap) string {
		return ""
	}, c.tagWatcher.AccessUnprotected())

	sq1 := &TestStatusQueue{state: map[status.Resource]any{}}
	setAndWait(t, c, sq1)
	assert.Equal(t, fetch(sq1), []string{"hello world"})

	c.status.UnsetQueue()

	// We should not get an update on the un-registered queue
	fakeCol.UpdateObject(Status{
		Obj:    &v1.ConfigMap{},
		Status: "hello world2",
	})
	assert.Equal(t, fetch(sq1), []string{"hello world"})

	// New queue should send new events, including existing state
	sq2 := &TestStatusQueue{state: map[status.Resource]any{}}
	setAndWait(t, c, sq2)
	assert.Equal(t, fetch(sq2), []string{"hello world2"})
	// And any new state
	fakeCol.UpdateObject(Status{
		Obj:    &v1.ConfigMap{},
		Status: "hello world3",
	})
	// New event, so this is eventually consistent
	assert.EventuallyEqual(t, func() []string {
		return fetch(sq2)
	}, []string{"hello world3"})
}

func setAndWait(t test.Failer, c *Controller, q status.Queue) {
	stop := test.NewStop(t)
	for _, syncer := range c.status.SetQueue(q) {
		syncer.WaitUntilSynced(stop)
	}
}

func setupControllerWithoutGatewayClasses(t *testing.T, objs ...runtime.Object) *Controller {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	controller := NewController(
		kc,
		func(class schema.GroupVersionResource, stop <-chan struct{}) bool {
			return false
		},
		controller.Options{KrtDebugger: krt.GlobalDebugHandler},
		nil)
	kc.RunAndWait(stop)
	go controller.Run(stop)
	cg := core.NewConfigGenTest(t, core.TestOptions{})
	controller.Reconcile(cg.PushContext())
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}
