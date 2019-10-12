// Copyright 2019 Istio Authors
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
	"errors"
	"sync/atomic"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/status"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/galley/pkg/testing/mock"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	k8sTesting "k8s.io/client-go/testing"
)

func TestNewSource(t *testing.T) {
	k := &mock.Kube{}
	for i := 0; i < 100; i++ {
		_ = fakeClient(k)
	}

	r := basicmeta.MustGet().KubeSource().Resources()

	_ = newOrFail(t, k, r, nil)
}

func TestStartTwice(t *testing.T) {
	// Create the source
	w, _, cl := createMocks()
	defer w.Stop()

	r := basicmeta.MustGet().KubeSource().Resources()
	s := newOrFail(t, cl, r, nil)

	// Start it once.
	_ = start(s)
	defer s.Stop()

	// Start again should fail
	s.Start()
}

func TestStartStop_WithStatusCtl(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create the source
	w, _, cl := createMocks()
	defer w.Stop()

	sc := &statusCtl{}
	r := basicmeta.MustGet().KubeSource().Resources()
	s := newOrFail(t, cl, r, sc)

	s.Start()
	g.Eventually(sc.hasStarted).Should(BeTrue())

	s.Stop()
	g.Eventually(sc.hasStopped).Should(BeTrue())
}

func TestStopTwiceShouldSucceed(t *testing.T) {
	// Create the source
	w, _, cl := createMocks()
	defer w.Stop()
	r := basicmeta.MustGet().KubeSource().Resources()
	s := newOrFail(t, cl, r, nil)

	// Start it once.
	_ = start(s)

	s.Stop()
	s.Stop()
}

func TestReport(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create the source
	w, _, cl := createMocks()
	defer w.Stop()

	sc := &statusCtl{}
	r := basicmeta.MustGet().KubeSource().Resources()
	s := newOrFail(t, cl, r, sc)

	s.Start()
	defer s.Stop()

	e := resource.Entry{
		Origin: &rt.Origin{
			Collection: basicmeta.Collection1,
			Name:       resource.NewName("foo", "bar"),
			Version:    resource.Version("v1"),
		},
	}
	m := msg.NewInternalError(&e, "foo")

	s.Update(diag.Messages{m})
	g.Expect(sc.latestReport()).To(Equal(diag.Messages{m}))
}

func TestEvents(t *testing.T) {
	g := NewGomegaWithT(t)

	w, wcrd, cl := createMocks()
	defer wcrd.Stop()
	defer w.Stop()

	r := basicmeta.MustGet().KubeSource().Resources()
	addCrdEvents(wcrd, r)

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
	))
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

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.AddFor(basicmeta.Collection1, toEntry(obj)),
	))

	acc.Clear()

	obj = obj.DeepCopy()
	obj.SetResourceVersion("rv2")

	w.Send(watch.Event{Type: watch.Modified, Object: obj})

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.UpdateFor(basicmeta.Collection1, toEntry(obj))))

	acc.Clear()

	// Make a copy so we can change it without affecting the original.
	objCopy := obj.DeepCopy()
	objCopy.SetResourceVersion("rv2")

	w.Send(watch.Event{Type: watch.Modified, Object: objCopy})
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	w.Send(watch.Event{Type: watch.Deleted, Object: obj})

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(basicmeta.Collection1, toEntry(obj))))
}

func TestEvents_WatchUpdatesStatusCtl(t *testing.T) {
	g := NewGomegaWithT(t)

	w, wcrd, cl := createMocks()
	defer wcrd.Stop()
	defer w.Stop()

	r := basicmeta.MustGet().KubeSource().Resources()
	addCrdEvents(wcrd, r)

	sc := &statusCtl{}

	// Create and start the source
	s := newOrFail(t, cl, r, sc)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
	))
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

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.AddFor(basicmeta.Collection1, toEntry(obj)),
	))

	g.Eventually(sc.latestStatusCall).ShouldNot(BeNil())
	g.Expect(sc.latestStatusCall()).To(Equal(&statusInput{
		col:     basicmeta.Collection1,
		name:    resource.NewName("ns", "i1"),
		version: "v1",
		status:  nil,
	}))
	acc.Clear()

	obj = obj.DeepCopy()
	obj.SetResourceVersion("rv2")
	obj.Object["status"] = "stat"

	w.Send(watch.Event{Type: watch.Modified, Object: obj})

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.UpdateFor(basicmeta.Collection1, toEntry(obj))))

	g.Expect(sc.latestStatusCall()).To(Equal(&statusInput{
		col:     basicmeta.Collection1,
		name:    resource.NewName("ns", "i1"),
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

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(basicmeta.Collection1, toEntry(obj))))
}

func TestEvents_CRDEventAfterFullSync(t *testing.T) {
	g := NewGomegaWithT(t)

	w, wcrd, cl := createMocks()
	defer wcrd.Stop()
	defer w.Stop()

	r := basicmeta.MustGet().KubeSource().Resources()
	addCrdEvents(wcrd, r)

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
	))

	acc.Clear()
	c := toCrd(r[0])
	c.ResourceVersion = "v2"
	wcrd.Send(watch.Event{
		Type:   watch.Modified,
		Object: c,
	})

	g.Eventually(acc.Events).Should(ContainElement(
		event.Event{Kind: event.Reset},
	))
}

func TestEvents_NonAddEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	w, wcrd, cl := createMocks()
	defer wcrd.Stop()
	defer w.Stop()

	r := basicmeta.MustGet().KubeSource().Resources()
	addCrdEvents(wcrd, r)
	c := toCrd(r[0])
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
	g := NewGomegaWithT(t)

	w, wcrd, cl := createMocks()
	defer wcrd.Stop()
	defer w.Stop()

	r := basicmeta.MustGet().KubeSource().Resources()
	addCrdEvents(wcrd, r)

	// Create and start the source
	s := newOrFail(t, cl, r, nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.Events).Should(BeEmpty())
}

func TestSource_WatcherFailsCreatingInformer(t *testing.T) {
	g := NewGomegaWithT(t)

	k := mock.NewKube()
	wcrd := mockCrdWatch(k.APIExtClientSet)

	r := basicmeta.MustGet().KubeSource().Resources()
	addCrdEvents(wcrd, r)

	k.AddResponse(nil, errors.New("no cheese found"))

	// Create and start the source
	s := newOrFail(t, k, r, nil)
	// Start/stop when informer is not created. It should not crash or cause errors.
	acc := start(s)

	// we should get a full sync event, even if the watcher doesn't properly start.
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
	))

	s.Stop()

	acc.Clear()
	wcrd.Stop()

	wcrd = mockCrdWatch(k.APIExtClientSet)
	addCrdEvents(wcrd, r)

	// Now start properly and get events
	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)
	w := mockWatch(cl)

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

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(basicmeta.Collection1, toEntry(obj)),
	))
}

func TestUpdateMessage_NoStatusController_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	k := &mock.Kube{}
	for i := 0; i < 100; i++ {
		_ = fakeClient(k)
	}

	r := basicmeta.MustGet().KubeSource().Resources()

	s := newOrFail(t, k, r, nil)

	s.Start()
	defer s.Stop()
	s.Update(diag.Messages{})
}

func newOrFail(t *testing.T, ifaces kube.Interfaces, r schema.KubeResources, sc status.Controller) *apiserver.Source {
	t.Helper()
	o := apiserver.Options{
		Resources:        r,
		ResyncPeriod:     0,
		Client:           ifaces,
		StatusController: sc,
	}
	s := apiserver.New(o)
	if s == nil {
		t.Fatal("Expected non nil source")
	}
	return s
}

func start(s *apiserver.Source) *fixtures.Accumulator {
	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	return acc
}

func createMocks() (*mock.Watch, *mock.Watch, *mock.Kube) {
	k := mock.NewKube()
	cl := fakeClient(k)
	w := mockWatch(cl)
	wcrd := mockCrdWatch(k.APIExtClientSet)
	return w, wcrd, k
}

func addCrdEvents(w *mock.Watch, res []schema.KubeResource) {
	for _, r := range res {
		w.Send(watch.Event{
			Object: toCrd(r),
			Type:   watch.Added,
		})
	}
}

func fakeClient(k *mock.Kube) *fake.FakeDynamicClient {
	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)
	return cl
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

func toEntry(obj *unstructured.Unstructured) *resource.Entry {
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name:        resource.NewName(obj.GetNamespace(), obj.GetName()),
			Labels:      obj.GetLabels(),
			Annotations: obj.GetAnnotations(),
			Version:     resource.Version(obj.GetResourceVersion()),
		},
		Item: &types.Struct{
			Fields: make(map[string]*types.Value),
		},
	}
}

func toCrd(r schema.KubeResource) *v1beta1.CustomResourceDefinition {
	return &v1beta1.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{
			Name:            r.Plural + "." + r.Group,
			ResourceVersion: "v1",
		},

		Spec: v1beta1.CustomResourceDefinitionSpec{
			Group: r.Group,
			Names: v1beta1.CustomResourceDefinitionNames{
				Plural: r.Plural,
				Kind:   r.Kind,
			},
			Versions: []v1beta1.CustomResourceDefinitionVersion{
				{
					Name: r.Version,
				},
			},
			Scope: v1beta1.NamespaceScoped,
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
	name    resource.Name
	version resource.Version
	status  interface{}
}

var _ status.Controller = &statusCtl{}

func (s *statusCtl) Start(*rt.Provider, []schema.KubeResource) {
	atomic.StoreInt32(&s.started, 1)
}

func (s *statusCtl) Stop() {
	atomic.StoreInt32(&s.stopped, 1)
}

func (s *statusCtl) UpdateResourceStatus(
	col collection.Name, name resource.Name, version resource.Version, status interface{}) {
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
