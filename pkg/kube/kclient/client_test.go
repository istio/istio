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

package kclient_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/exp/slices"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func TestHasSynced(t *testing.T) {
	handled := atomic.NewInt64(0)
	c := kube.NewFakeClient()
	deployments := kclient.New[*appsv1.Deployment](c)
	obj1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "1", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{MinReadySeconds: 1},
	}
	clienttest.Wrap(t, deployments).Create(obj1)
	deployments.AddEventHandler(controllers.EventHandler[*appsv1.Deployment]{
		AddFunc: func(obj *appsv1.Deployment) {
			handled.Inc()
		},
	})
	c.RunAndWait(test.NewStop(t))
	retry.UntilOrFail(t, deployments.HasSynced, retry.Timeout(time.Second*2), retry.Delay(time.Millisecond))
	// This checks sync worked properly. This MUST be immediately available, not eventually
	assert.Equal(t, handled.Load(), 1)
}

func TestClient(t *testing.T) {
	tracker := newTracker(t)
	c := kube.NewFakeClient()
	deployments := kclient.NewFiltered[*appsv1.Deployment](c, kclient.Filter{ObjectFilter: func(t any) bool {
		return t.(*appsv1.Deployment).Namespace == "default"
	}})
	deployments.AddEventHandler(controllers.EventHandler[*appsv1.Deployment]{
		AddFunc: func(obj *appsv1.Deployment) {
			tracker.Record("add/" + obj.Name)
		},
		UpdateFunc: func(oldObj, newObj *appsv1.Deployment) {
			tracker.Record("update/" + newObj.Name)
		},
		DeleteFunc: func(obj *appsv1.Deployment) {
			tracker.Record("delete/" + obj.Name)
		},
	})
	tester := clienttest.Wrap(t, deployments)

	c.RunAndWait(test.NewStop(t))
	obj1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "1", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{MinReadySeconds: 1},
	}
	obj2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "2", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{MinReadySeconds: 10},
	}
	obj3 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "3", Namespace: "not-default"},
		Spec:       appsv1.DeploymentSpec{MinReadySeconds: 100},
	}

	// Create object, make sure we can see it
	tester.Create(obj1)
	// Client is cached, so its only eventually consistent
	tracker.Wait("add/1")
	assert.Equal(t, tester.Get(obj1.Name, obj1.Namespace), obj1)
	assert.Equal(t, tester.List("", klabels.Everything()), []*appsv1.Deployment{obj1})
	assert.Equal(t, tester.List(obj1.Namespace, klabels.Everything()), []*appsv1.Deployment{obj1})

	// Update object, should see the update...
	obj1.Spec.MinReadySeconds = 2
	tester.Update(obj1)
	tracker.Wait("update/1")
	assert.Equal(t, tester.Get(obj1.Name, obj1.Namespace), obj1)

	// Create some more objects
	tester.Create(obj3)
	tester.Create(obj2)
	tracker.Wait("add/2")
	assert.Equal(t, tester.Get(obj2.Name, obj2.Namespace), obj2)
	// We should not see obj3, it is filtered

	deploys := tester.List(obj1.Namespace, klabels.Everything())
	slices.SortFunc(deploys, func(a, b *appsv1.Deployment) bool {
		return a.Name < b.Name
	})
	assert.Equal(t, deploys, []*appsv1.Deployment{obj1, obj2})
	assert.Equal(t, tester.Get(obj3.Name, obj3.Namespace), nil)

	tester.Delete(obj3.Name, obj3.Namespace)
	tester.Delete(obj2.Name, obj2.Namespace)
	tester.Delete(obj1.Name, obj1.Namespace)
	tracker.Wait("delete/2")
	tracker.Wait("delete/1")
	assert.Equal(t, tester.List(obj1.Namespace, klabels.Everything()), nil)
}

type tracker struct {
	t      test.Failer
	mu     sync.Mutex
	events []string
}

func newTracker(t test.Failer) *tracker {
	return &tracker{t: t}
}

func (t *tracker) Record(event string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *tracker) Wait(event string) {
	t.t.Helper()
	retry.UntilSuccessOrFail(t.t, func() error {
		t.mu.Lock()
		defer t.mu.Unlock()
		if len(t.events) == 0 {
			return fmt.Errorf("no events")
		}
		if t.events[0] != event {
			return fmt.Errorf("got events %v, want %v", t.events, event)
		}
		// clear the event
		t.events = t.events[1:]
		return nil
	}, retry.Timeout(time.Second*5))
}
