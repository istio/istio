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
package apiserver_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
	uatomic "go.uber.org/atomic"
	kube3 "istio.io/istio/pkg/config/legacy/source/kube"
	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	k8sTesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/status"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

func TestStartTwice(t *testing.T) {
	r := basicmeta2.MustGet().KubeCollections()
	s := newOrFail(t, kubelib.NewFakeClient(), r, nil)

	// Start it once.
	_ = start(s)
	defer s.Stop()

	// Start again should fail
	s.Start()
}

func TestStartStop_WithStatusCtl(t *testing.T) {
	g := NewWithT(t)

	sc := &statusCtl{}
	r := basicmeta2.MustGet().KubeCollections()
	s := newOrFail(t, kubelib.NewFakeClient(), r, sc)

	s.Start()
	g.Eventually(sc.hasStarted).Should(BeTrue())

	s.Stop()
	g.Eventually(sc.hasStopped).Should(BeTrue())
}

func TestStopTwiceShouldSucceed(t *testing.T) {
	r := basicmeta2.MustGet().KubeCollections()
	s := newOrFail(t, kubelib.NewFakeClient(), r, nil)

	// Start it once.
	_ = start(s)

	s.Stop()
	s.Stop()
}

func TestReport(t *testing.T) {
	g := NewWithT(t)

	sc := &statusCtl{}
	r := basicmeta2.MustGet().KubeCollections()
	s := newOrFail(t, kubelib.NewFakeClient(), r, sc)

	s.Start()
	defer s.Stop()

	e := resource.Instance{
		Origin: &kube3.Origin{
			Collection: basicmeta2.K8SCollection1.Name(),
			FullName:   resource.NewFullName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}
	m := msg.NewInternalError(&e, "foo")

	s.Update(diag.Messages{m})
	g.Expect(sc.latestReport()).To(Equal(diag.Messages{m}))
}

func TestEvents(t *testing.T) {
	g := NewWithT(t)

	w, wcrd, cl := createMocks(t)
	defer wcrd.Stop()
	defer w.Stop()

	r := basicmeta2.MustGet().KubeCollections()
	addCrdEvents(wcrd, r.All())

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.FullSyncFor(basicmeta2.K8SCollection1))
	acc.Clear()

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "testdata.istio.io/v1alpha1",
			"kind":       "Kind1",
			"metadata": map[string]interface{}{
				"name":            "i1",
				"namespace":       "ns",
				"resourceVersion": "v1",
			},
			"spec": map[string]interface{}{},
		},
	}

	obj = obj.DeepCopy()
	w.Send(watch.Event{Type: watch.Added, Object: obj})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.AddFor(basicmeta2.K8SCollection1,
		toEntry(obj, basicmeta2.K8SCollection1.Resource())))

	acc.Clear()

	obj = obj.DeepCopy()
	obj.SetResourceVersion("rv2")

	w.Send(watch.Event{Type: watch.Modified, Object: obj})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.UpdateFor(basicmeta2.K8SCollection1,
		toEntry(obj, basicmeta2.K8SCollection1.Resource())))

	acc.Clear()

	// Make a copy so we can change it without affecting the original.
	objCopy := obj.DeepCopy()
	objCopy.SetResourceVersion("rv2")

	w.Send(watch.Event{Type: watch.Modified, Object: objCopy})
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	w.Send(watch.Event{Type: watch.Deleted, Object: obj})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.DeleteForResource(basicmeta2.K8SCollection1,
		toEntry(obj, basicmeta2.K8SCollection1.Resource())))
}

func TestEvents_WatchUpdatesStatusCtl(t *testing.T) {
	g := NewWithT(t)

	w, wcrd, cl := createMocks(t)

	r := basicmeta2.MustGet().KubeCollections()
	addCrdEvents(wcrd, r.All())

	sc := &statusCtl{}

	// Create and start the source
	s := newOrFail(t, cl, r, sc)
	acc := start(s)
	defer s.Stop()

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.FullSyncFor(basicmeta2.K8SCollection1))
	acc.Clear()

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "testdata.istio.io/v1alpha1",
			"kind":       "Kind1",
			"metadata": map[string]interface{}{
				"name":            "i1",
				"namespace":       "ns",
				"resourceVersion": "v1",
			},
			"spec": map[string]interface{}{},
		},
	}

	obj = obj.DeepCopy()
	w.Send(watch.Event{Type: watch.Added, Object: obj})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.AddFor(basicmeta2.K8SCollection1,
		toEntry(obj, basicmeta2.K8SCollection1.Resource())))

	g.Eventually(sc.latestStatusCall).ShouldNot(BeNil())
	g.Expect(sc.latestStatusCall()).To(Equal(&statusInput{
		col:     basicmeta2.K8SCollection1.Name(),
		name:    resource.NewFullName("ns", "i1"),
		version: "v1",
		status:  nil,
	}))
	acc.Clear()

	obj = obj.DeepCopy()
	obj.SetResourceVersion("rv2")
	obj.Object["status"] = "stat"

	w.Send(watch.Event{Type: watch.Modified, Object: obj})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.UpdateFor(basicmeta2.K8SCollection1,
		toEntry(obj, basicmeta2.K8SCollection1.Resource())))

	g.Expect(sc.latestStatusCall()).To(Equal(&statusInput{
		col:     basicmeta2.K8SCollection1.Name(),
		name:    resource.NewFullName("ns", "i1"),
		version: "rv2",
		status:  "stat",
	}))

	acc.Clear()

	// Make a copy so we can change it without affecting the original.
	objCopy := obj.DeepCopy()
	objCopy.SetResourceVersion("rv2")

	w.Send(watch.Event{Type: watch.Modified, Object: objCopy})
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	w.Send(watch.Event{Type: watch.Deleted, Object: obj})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.DeleteForResource(basicmeta2.K8SCollection1,
		toEntry(obj, basicmeta2.K8SCollection1.Resource())))
}

func TestEvents_CRDEventAfterFullSync(t *testing.T) {
	_, wcrd, cl := createMocks(t)

	r := basicmeta2.MustGet().KubeCollections()
	addCrdEvents(wcrd, r.All())

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.FullSyncFor(basicmeta2.K8SCollection1))

	acc.Clear()
	c := toCrd(r.All()[0])
	c.ResourceVersion = "v2"
	wcrd.Send(watch.Event{
		Type:   watch.Modified,
		Object: c,
	})

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.Event{Kind: event.Reset})
}

func TestEvents_NonAddEvent(t *testing.T) {
	g := NewWithT(t)

	_, wcrd, cl := createMocks(t)

	r := basicmeta2.MustGet().KubeCollections()
	addCrdEvents(wcrd, r.All())
	c := toCrd(r.All()[0])
	c.ResourceVersion = "v2"
	wcrd.Send(watch.Event{
		Type:   watch.Modified,
		Object: c,
	})

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.Events).Should(ContainElement(
		event.Event{Kind: event.Reset},
	))
}

func TestEvents_NoneForDisabled(t *testing.T) {
	g := NewWithT(t)

	_, wcrd, cl := createMocks(t)

	r := basicmeta2.MustGet().KubeCollections()
	addCrdEvents(wcrd, r.All())

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.Events).Should(BeEmpty())
}

func TestSource_WatcherFailsCreatingInformer(t *testing.T) {
	// Setup a lock so we can dynamically change the watch returned
	mu := sync.Mutex{}
	errRet := uatomic.NewString("")
	k := kubelib.NewFakeClient()

	// Setup mock watch
	w := mock.NewWatch()
	k.Dynamic().(*fake.FakeDynamicClient).PrependWatchReactor("*", func(_ k8sTesting.Action) (handled bool, ret watch.Interface, err error) {
		mu.Lock()
		defer mu.Unlock()
		return true, w, nil
	})
	t.Cleanup(w.Stop)

	wcrd := mockCrdWatch(k.Ext().(*extfake.Clientset))
	t.Cleanup(wcrd.Stop)

	r := basicmeta2.MustGet().KubeCollections()
	addCrdEvents(wcrd, r.All())
	k.Dynamic().(*fake.FakeDynamicClient).PrependReactor("*", "*", func(action k8sTesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		e := errRet.Load() // Fetch the error and wipe it out
		errRet.Store("")
		if e == "" {
			return false, nil, nil
		}
		return true, nil, fmt.Errorf(e)
	})

	errRet.Store("no cheese found")

	// Create and start the source
	s := newOrFail(t, k, r, nil)
	// Start/stop when informer is not created. It should not crash or cause errors.
	acc := start(s)

	// we should get a full sync event, even if the watcher doesn't properly start.
	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc, event.FullSyncFor(basicmeta2.K8SCollection1))

	s.Stop()

	acc.Clear()
	wcrd.Stop()

	wcrd = mockCrdWatch(k.Ext().(*extfake.Clientset))
	addCrdEvents(wcrd, r.All())

	// Now start properly and get events
	errRet.Store("")
	mu.Lock()
	w = mock.NewWatch()
	mu.Unlock()

	s.Start()

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "testdata.istio.io/v1alpha1",
			"kind":       "Kind1",
			"metadata": map[string]interface{}{
				"name":            "i1",
				"namespace":       "ns",
				"resourceVersion": "v1",
			},
			"spec": map[string]interface{}{},
		},
	}
	obj = obj.DeepCopy()

	w.Send(watch.Event{Type: watch.Added, Object: obj})

	defer s.Stop()

	fixtures2.ExpectEventsWithoutOriginsEventually(t, acc,
		event.AddFor(basicmeta2.K8SCollection1, toEntry(obj, basicmeta2.K8SCollection1.Resource())),
		event.FullSyncFor(basicmeta2.K8SCollection1))
}

func TestUpdateMessage_NoStatusController_Panic(t *testing.T) {
	g := NewWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	r := basicmeta2.MustGet().KubeCollections()

	s := newOrFail(t, kubelib.NewFakeClient(), r, nil)

	start(s)
	defer s.Stop()
	s.Update(diag.Messages{})
}

func newOrFail(t *testing.T, client kubelib.Client, r collection.Schemas, sc status.Controller) *apiserver.Source {
	t.Helper()
	o := apiserver.Options{
		Schemas:          r,
		ResyncPeriod:     0,
		Client:           kube.NewInterfacesFromClient(client),
		StatusController: sc,
	}
	s := apiserver.New(o)
	if s == nil {
		t.Fatal("Expected non nil source")
	}
	return s
}

func start(s *apiserver.Source) *fixtures2.Accumulator {
	acc := &fixtures2.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	return acc
}

func createMocks(t test.Failer) (*mock.Watch, *mock.Watch, kubelib.ExtendedClient) {
	c := kubelib.NewFakeClient()
	w := mockWatch(c.Dynamic().(*fake.FakeDynamicClient))
	t.Cleanup(w.Stop)
	wcrd := mockCrdWatch(c.Ext().(*extfake.Clientset))
	t.Cleanup(wcrd.Stop)
	return w, wcrd, c
}

func addCrdEvents(w *mock.Watch, res []collection.Schema) {
	for _, r := range res {
		w.Send(watch.Event{
			Object: toCrd(r),
			Type:   watch.Added,
		})
	}
}

func mockWatch(cl *fake.FakeDynamicClient) *mock.Watch {
	w := mock.NewWatch()
	cl.PrependWatchReactor("*", func(_ k8sTesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})
	return w
}

func mockCrdWatch(cl *extfake.Clientset) *mock.Watch {
	w := mock.NewWatch()
	cl.PrependWatchReactor("*", func(_ k8sTesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})
	return w
}

func toEntry(obj *unstructured.Unstructured, schema resource2.Schema) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName:    resource.NewFullName(resource.Namespace(obj.GetNamespace()), resource.LocalName(obj.GetName())),
			Labels:      obj.GetLabels(),
			Annotations: obj.GetAnnotations(),
			Version:     resource.Version(obj.GetResourceVersion()),
			Schema:      schema,
		},
		Message: &types.Struct{
			Fields: make(map[string]*types.Value),
		},
	}
}

func toCrd(schema collection.Schema) *apiextensions.CustomResourceDefinition {
	r := schema.Resource()
	return &apiextensions.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{
			Name:            r.Plural() + "." + r.Group(),
			ResourceVersion: "v1",
		},

		Spec: apiextensions.CustomResourceDefinitionSpec{
			Group: r.Group(),
			Names: apiextensions.CustomResourceDefinitionNames{
				Plural: r.Plural(),
				Kind:   r.Kind(),
			},
			Versions: []apiextensions.CustomResourceDefinitionVersion{
				{
					Name: r.Version(),
				},
			},
			Scope: apiextensions.NamespaceScoped,
		},
	}
}

type statusCtl struct {
	started int32
	stopped int32

	lastStatusInput atomic.Value
	lastReport      atomic.Value
}

type statusInput struct {
	col     collection.Name
	name    resource.FullName
	version resource.Version
	status  interface{}
}

var _ status.Controller = &statusCtl{}

func (s *statusCtl) Start(*rt.Provider, []collection.Schema) {
	atomic.StoreInt32(&s.started, 1)
}

func (s *statusCtl) Stop() {
	atomic.StoreInt32(&s.stopped, 1)
}

func (s *statusCtl) UpdateResourceStatus(
	col collection.Name, name resource.FullName, version resource.Version, status interface{}) {
	i := &statusInput{
		col:     col,
		name:    name,
		version: version,
		status:  status,
	}
	s.lastStatusInput.Store(i)
}

func (s *statusCtl) Report(messages diag.Messages) {
	s.lastReport.Store(messages)
}

func (s *statusCtl) hasStarted() bool {
	return atomic.LoadInt32(&s.started) != 0
}

func (s *statusCtl) hasStopped() bool {
	return atomic.LoadInt32(&s.stopped) != 0
}

func (s *statusCtl) latestStatusCall() *statusInput {
	i := s.lastStatusInput.Load()
	if i == nil {
		return nil
	}
	return i.(*statusInput)
}

func (s *statusCtl) latestReport() diag.Messages {
	i := s.lastReport.Load()
	if i == nil {
		return nil
	}
	return i.(diag.Messages)
}
