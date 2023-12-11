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
	"reflect"
	"testing"
	"time"

	"go.uber.org/atomic"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	istioclient "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func TestSwappingClient(t *testing.T) {
	t.Run("CRD partially ready", func(t *testing.T) {
		stop := test.NewStop(t)
		c := kube.NewFakeClient()
		wasm := kclient.NewDelayedInformer[controllers.Object](c, gvr.WasmPlugin, kubetypes.StandardInformer, kubetypes.Filter{})
		wt := clienttest.NewWriter[*istioclient.WasmPlugin](t, c)
		tracker := assert.NewTracker[string](t)
		wasm.AddEventHandler(clienttest.TrackerHandler(tracker))
		go constantlyAccessForRaceDetection(stop, wasm)

		// CRD and Delayed client are ready to go by the time we start informers
		clienttest.MakeCRD(t, c, gvr.WasmPlugin)
		c.RunAndWait(stop)

		wt.Create(&istioclient.WasmPlugin{
			ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: "default"},
		})
		assert.EventuallyEqual(t, func() int {
			return len(wasm.List("", klabels.Everything()))
		}, 1)
		tracker.WaitOrdered("add/name")
	})
	t.Run("CRD fully ready", func(t *testing.T) {
		stop := test.NewStop(t)
		c := kube.NewFakeClient()

		// Only CRD is ready to go by the time we start informers
		clienttest.MakeCRD(t, c, gvr.WasmPlugin)
		c.RunAndWait(stop)

		// Now that CRD is synced, we create the client
		wasm := kclient.NewDelayedInformer[controllers.Object](c, gvr.WasmPlugin, kubetypes.StandardInformer, kubetypes.Filter{})
		wt := clienttest.NewWriter[*istioclient.WasmPlugin](t, c)
		tracker := assert.NewTracker[string](t)
		wasm.AddEventHandler(clienttest.TrackerHandler(tracker))
		go constantlyAccessForRaceDetection(stop, wasm)
		c.RunAndWait(stop)
		kube.WaitForCacheSync("test", test.NewStop(t), wasm.HasSynced)

		wt.Create(&istioclient.WasmPlugin{
			ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: "default"},
		})
		assert.EventuallyEqual(t, func() int {
			return len(wasm.List("", klabels.Everything()))
		}, 1)
		tracker.WaitOrdered("add/name")
	})
	t.Run("CRD not ready", func(t *testing.T) {
		stop := test.NewStop(t)
		c := kube.NewFakeClient()

		// Client created before CRDs are ready
		wasm := kclient.NewDelayedInformer[controllers.Object](c, gvr.WasmPlugin, kubetypes.StandardInformer, kubetypes.Filter{})
		tracker := assert.NewTracker[string](t)
		wasm.AddEventHandler(clienttest.TrackerHandler(tracker))
		go constantlyAccessForRaceDetection(stop, wasm)
		c.RunAndWait(stop)
		kube.WaitForCacheSync("test", test.NewStop(t), wasm.HasSynced)

		// List should return empty
		assert.Equal(t, len(wasm.List("", klabels.Everything())), 0)

		// Now we add the CRD
		clienttest.MakeCRD(t, c, gvr.WasmPlugin)
		// This is pretty bad, but purely works around https://github.com/kubernetes/kubernetes/issues/95372
		// which impacts only the fake client.
		// Basically if the Create happens between the List and Watch it is lost. But we don't know when
		// this occurs, so we just retry
		cl := kclient.NewWriteClient[*istioclient.WasmPlugin](c)
		retry.UntilSuccessOrFail(t, func() error {
			cl.Create(&istioclient.WasmPlugin{
				ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: "default"},
			})
			for attempt := 0; attempt < 10; attempt++ {
				l := wasm.List("", klabels.Everything())
				if len(l) == 1 {
					return nil
				}
				time.Sleep(time.Millisecond * 2)
			}
			cl.Delete("name", "default")
			return fmt.Errorf("expected one item in list")
		})
		tracker.WaitOrdered("add/name")
	})
	t.Run("watcher not run ready", func(t *testing.T) {
		stop := test.NewStop(t)
		c := kube.NewFakeClient()

		// Client created before CRDs are ready
		wasm := kclient.NewDelayedInformer[controllers.Object](c, gvr.WasmPlugin, kubetypes.StandardInformer, kubetypes.Filter{})
		wt := clienttest.NewWriter[*istioclient.WasmPlugin](t, c)
		tracker := assert.NewTracker[string](t)
		wasm.AddEventHandler(clienttest.TrackerHandler(tracker))
		go constantlyAccessForRaceDetection(stop, wasm)

		assert.Equal(t, wasm.HasSynced(), false)

		// List should return empty
		assert.Equal(t, len(wasm.List("", klabels.Everything())), 0)

		// Now we add the CRD
		clienttest.MakeCRD(t, c, gvr.WasmPlugin)

		// Start everything up
		c.RunAndWait(stop)
		wt.Create(&istioclient.WasmPlugin{
			ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: "default"},
		})
		assert.EventuallyEqual(t, func() int {
			return len(wasm.List("", klabels.Everything()))
		}, 1)
		tracker.WaitOrdered("add/name")
	})
}

// setup some calls to ensure we trigger the race detector, if there was a race.
func constantlyAccessForRaceDetection(stop chan struct{}, wt kclient.Untyped) {
	for {
		select {
		case <-time.After(time.Millisecond):
		case <-stop:
			return
		}
		_ = wt.List("", klabels.Everything())
	}
}

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
	tracker := assert.NewTracker[string](t)
	c := kube.NewFakeClient()
	deployments := kclient.NewFiltered[*appsv1.Deployment](c, kclient.Filter{ObjectFilter: func(t any) bool {
		return t.(*appsv1.Deployment).Spec.MinReadySeconds < 100
	}})
	deployments.AddEventHandler(clienttest.TrackerHandler(tracker))
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
		ObjectMeta: metav1.ObjectMeta{Name: "3", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{MinReadySeconds: 100},
	}

	// Create object, make sure we can see it
	tester.Create(obj1)
	// Client is cached, so its only eventually consistent
	tracker.WaitOrdered("add/1")
	assert.Equal(t, tester.Get(obj1.Name, obj1.Namespace), obj1)
	assert.Equal(t, tester.List("", klabels.Everything()), []*appsv1.Deployment{obj1})
	assert.Equal(t, tester.List(obj1.Namespace, klabels.Everything()), []*appsv1.Deployment{obj1})

	// Update object, should see the update...
	obj1.Spec.MinReadySeconds = 2
	tester.Update(obj1)
	tracker.WaitOrdered("update/1")
	assert.Equal(t, tester.Get(obj1.Name, obj1.Namespace), obj1)

	// Create some more objects
	tester.Create(obj3)
	tester.Create(obj2)
	tracker.WaitOrdered("add/2")
	assert.Equal(t, tester.Get(obj2.Name, obj2.Namespace), obj2)
	// We should not see obj3, it is filtered

	deploys := tester.List(obj1.Namespace, klabels.Everything())
	slices.SortBy(deploys, func(a *appsv1.Deployment) string {
		return a.Name
	})
	assert.Equal(t, deploys, []*appsv1.Deployment{obj1, obj2})
	assert.Equal(t, tester.Get(obj3.Name, obj3.Namespace), nil)

	tester.Delete(obj3.Name, obj3.Namespace)
	tester.Delete(obj2.Name, obj2.Namespace)
	tester.Delete(obj1.Name, obj1.Namespace)
	tracker.WaitOrdered("delete/2", "delete/1")
	assert.Equal(t, tester.List(obj1.Namespace, klabels.Everything()), nil)

	// Create some more objects again
	tester.Create(obj3)
	tester.Create(obj2)
	tracker.WaitOrdered("add/2")
	assert.Equal(t, tester.Get(obj2.Name, obj2.Namespace), obj2)

	// Was filtered, now its not. Should get an Add
	obj3.Spec.MinReadySeconds = 5
	tester.Update(obj3)
	tracker.WaitOrdered("add/3")
	assert.Equal(t, tester.Get(obj3.Name, obj3.Namespace), obj3)

	// Was allowed, now its not. Should get a Delete
	obj3.Spec.MinReadySeconds = 150
	tester.Update(obj3)
	tracker.WaitOrdered("delete/3")
	assert.Equal(t, tester.Get(obj3.Name, obj3.Namespace), nil)
}

func TestErrorHandler(t *testing.T) {
	mt := monitortest.New(t)
	c := kube.NewFakeClient()
	// Prevent List from succeeding
	c.Kube().(*fake.Clientset).Fake.PrependReactor("*", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("nope, out of luck")
	})
	deployments := kclient.New[*appsv1.Deployment](c)
	deployments.Start(test.NewStop(t))
	mt.Assert("controller_sync_errors_total", map[string]string{"cluster": "fake"}, monitortest.AtLeast(1))
}

func TestToOpts(t *testing.T) {
	test.SetForTest(t, &features.InformerWatchNamespace, "istio-system")
	c := kube.NewFakeClient()
	cases := []struct {
		name   string
		gvr    schema.GroupVersionResource
		filter kclient.Filter
		want   kubetypes.InformerOptions
	}{
		{
			name: "watch pods in the foo namespace",
			gvr:  gvr.Pod,
			filter: kclient.Filter{
				Namespace: "foo",
			},
			want: kubetypes.InformerOptions{
				Namespace: "foo",
				Cluster:   c.ClusterID(),
			},
		},
		{
			name: "watch pods in the InformerWatchNamespace",
			gvr:  gvr.Pod,
			want: kubetypes.InformerOptions{
				Namespace: features.InformerWatchNamespace,
				Cluster:   c.ClusterID(),
			},
		},
		{
			name: "watch namespaces",
			gvr:  gvr.Namespace,
			want: kubetypes.InformerOptions{
				Namespace: "",
				Cluster:   c.ClusterID(),
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := kclient.ToOpts(c, tt.gvr, tt.filter)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToOpts: got %v, want %v", got, tt.want)
			}
		})
	}
}
