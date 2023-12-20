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

package kube

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"go.uber.org/atomic"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage/names"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	gatewayapifake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"

	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube/informerfactory"
	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// NewFakeClient creates a new, fake, client
func NewFakeClient(objects ...runtime.Object) CLIClient {
	c := &client{
		informerWatchesPending: atomic.NewInt32(0),
		clusterID:              "fake",
	}
	c.config = &rest.Config{
		Host: "server",
	}

	c.informerFactory = informerfactory.NewSharedInformerFactory()

	s := FakeIstioScheme
	fmf := newFieldManagerFacotry(s)
	merger := fakeMerger{
		scheme: s,
	}

	f := fake.NewSimpleClientset(objects...)
	insertPatchReactor(f, fmf)
	merger.Merge(f)
	c.kube = f

	c.metadata = metadatafake.NewSimpleMetadataClient(s)
	df := dynamicfake.NewSimpleDynamicClient(s)
	insertPatchReactor(df, fmf)
	merger.MergeDynamic(df)
	c.dynamic = df

	ifc := istiofake.NewSimpleClientset()
	insertPatchReactor(ifc, fmf)
	merger.Merge(ifc)
	c.istio = ifc

	gf := gatewayapifake.NewSimpleClientset()
	insertPatchReactor(gf, fmf)
	merger.Merge(gf)
	c.gatewayapi = gf
	c.extSet = extfake.NewSimpleClientset()

	// https://github.com/kubernetes/kubernetes/issues/95372
	// There is a race condition in the client fakes, where events that happen between the List and Watch
	// of an informer are dropped. To avoid this, we explicitly manage the list and watch, ensuring all lists
	// have an associated watch before continuing.
	// This would likely break any direct calls to List(), but for now our tests don't do that anyways. If we need
	// to in the future we will need to identify the Lists that have a corresponding Watch, possibly by looking
	// at created Informers
	// an atomic.Int is used instead of sync.WaitGroup because wg.Add and wg.Wait cannot be called concurrently
	listReactor := func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		c.informerWatchesPending.Inc()
		return false, nil, nil
	}
	watchReactor := func(tracker clienttesting.ObjectTracker) func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
		return func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
			gvr := action.GetResource()
			ns := action.GetNamespace()
			watch, err := tracker.Watch(gvr, ns)
			if err != nil {
				return false, nil, err
			}
			c.informerWatchesPending.Dec()
			return true, watch, nil
		}
	}
	for _, fc := range []fakeClient{
		c.kube.(*fake.Clientset),
		c.istio.(*istiofake.Clientset),
		c.gatewayapi.(*gatewayapifake.Clientset),
		c.dynamic.(*dynamicfake.FakeDynamicClient),
		c.metadata.(*metadatafake.FakeMetadataClient),
	} {
		fc.PrependWatchReactor("*", watchReactor(fc.Tracker()))
	}
	df.PrependReactor("list", "*", listReactor)
	c.metadata.(*metadatafake.FakeMetadataClient).PrependReactor("list", "*", listReactor)

	c.fastSync = true

	c.version = lazy.NewWithRetry(c.kube.Discovery().ServerVersion)

	if NewCrdWatcher != nil {
		c.crdWatcher = NewCrdWatcher(c)
	}

	return c
}

func NewFakeClientWithVersion(minor string, objects ...runtime.Object) CLIClient {
	c := NewFakeClient(objects...).(*client)
	c.Kube().Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &kubeVersion.Info{Major: "1", Minor: minor, GitVersion: fmt.Sprintf("v1.%v.0", minor)}
	return c
}

type fakeClient interface {
	PrependReactor(verb, resource string, reaction clienttesting.ReactionFunc)
	PrependWatchReactor(resource string, reaction clienttesting.WatchReactionFunc)
	Tracker() clienttesting.ObjectTracker
}

func insertPatchReactor(f clienttesting.FakeClient, fmf *fieldManagerFactory) {
	f.PrependReactor(
		"patch",
		"*",
		func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
			pa := action.(clienttesting.PatchAction)
			if pa.GetPatchType() == types.ApplyPatchType {
				// Apply patches are supposed to upsert, but fake client fails if the object doesn't exist,
				// if an apply patch occurs for a deployment that doesn't yet exist, create it.
				// However, we already hold the fakeclient lock, so we can't use the front door.
				// rfunc := clienttesting.ObjectReaction(f.Tracker())
				original, err := f.Tracker().Get(pa.GetResource(), pa.GetNamespace(), pa.GetName())
				var isNew bool
				mygvk := gvk.MustFromGVR(pa.GetResource()).Kubernetes()
				if kerrors.IsNotFound(err) || original == nil {
					isNew = true
					original = kubeclient.GVRToObject(pa.GetResource())
					d := original.(metav1.Object)
					d.SetName(pa.GetName())
					d.SetNamespace(pa.GetNamespace())
					e := original.(schema.ObjectKind)
					e.SetGroupVersionKind(mygvk)
				}

				applyObj := &unstructured.Unstructured{Object: map[string]interface{}{}}
				if err := json.Unmarshal(pa.GetPatch(), &applyObj.Object); err != nil {
					log.Errorf("error decoding YAML: %v", err)
					return false, nil, err
				}
				// TODO: I'm not 100% sure this should always force, but we lose the apply option...
				res, err := fmf.FieldManager(mygvk).Apply(original, applyObj, "istio", true)
				if err != nil {
					log.Errorf("error applying patch: %v", err)
					return false, res, err
				}

				// field manager outputs typed objects.  We might need unustructured here.
				var typedResult runtime.Object
				if _, ok := f.(*dynamicfake.FakeDynamicClient); ok {
					typedResult = &unstructured.Unstructured{}
					err = fmf.schema.Convert(res, typedResult, nil)
					if err != nil {
						log.Errorf("error converting object (%v), fakeclients may become inconsistent: %v", res, err)
						return false, nil, err
					}
				} else {
					typedResult = res
				}

				if isNew {
					// TODO: add name generation here
					generateNameOnCreate(typedResult)
					err = f.Tracker().Create(pa.GetResource(), typedResult, pa.GetNamespace())
				} else if !reflect.DeepEqual(original, typedResult) {
					err = f.Tracker().Update(pa.GetResource(), typedResult, pa.GetNamespace())
				}

				return true, typedResult, err
			}
			return false, nil, nil
		},
	)
}

func actionKey(action clienttesting.Action) string {
	out := strings.Builder{}
	out.WriteString(action.GetVerb())
	out.WriteString("/")
	out.WriteString(action.GetResource().String())
	out.WriteString("/")
	out.WriteString(action.GetNamespace())
	if n, ok := action.(named); ok {
		out.WriteString("/")
		out.WriteString(n.GetName())
	}
	return out.String()
}

type named interface {
	GetName() string
}
type fakeMerger struct {
	inProgress sets.Set[string]
	mergeLock  sync.RWMutex
	initOnce   sync.Once
	alertList  []clienttesting.FakeClient
	scheme     *runtime.Scheme
	dynamic    *dynamicfake.FakeDynamicClient
}

func (fm *fakeMerger) init() {
	fm.initOnce.Do(func() {
		fm.mergeLock.Lock()
		defer fm.mergeLock.Unlock()
		fm.inProgress = sets.Set[string]{}
	})
}

func (fm *fakeMerger) propagateReactionSingle(destination clienttesting.FakeClient, action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
	// prevent recursion with inProgress
	// TODO: this is probably excessive locking
	key := actionKey(action)
	fm.mergeLock.RLock()
	if fm.inProgress.Contains(key) {
		// this is an action 'echoing' back after we propagated it.  drop it.
		fm.mergeLock.RUnlock()
		return false, nil, nil
	}
	fm.mergeLock.RUnlock()
	fm.mergeLock.Lock()
	fm.inProgress.Insert(key)
	fm.mergeLock.Unlock()
	// if action is update or create, it includes a runtim.Object, which needs to be
	// passed to Invokes (either as unstructured [in the case of dynamic], or typed),
	// otherwise the obj var is ignored, and is traditionally defaulted to metav1.status
	defaultObj := &metav1.Status{Status: "metadata get fail"}
	var tObj, uObj runtime.Object
	if o, ok := action.(objectiveAction); ok {
		uObj = o.GetObject()
		// ensure object kind is properly set
		if uObj.GetObjectKind().GroupVersionKind().Empty() {
			if myGVK, ok := gvk.FromGVR(action.GetResource()); ok {
				uObj.GetObjectKind().SetGroupVersionKind(myGVK.Kubernetes())
			} else {
				log.Warnf("Could not detect kind for %v, proceeding with empty GVK", action.GetResource())
			}
		}
		if _, ok := destination.(*dynamicfake.FakeDynamicClient); ok {
			tObj = &unstructured.Unstructured{}
		} else {
			if _, ok := collections.All.FindByGroupVersionResource(action.GetResource()); ok {
				tObj = kubeclient.GVRToObject(action.GetResource())
			} else {
				return false, nil, fmt.Errorf("cannot find gvk for %v", action.GetResource())
			}
		}
		// convert the object to the required type
		err = fm.scheme.Convert(uObj, tObj, nil)
		if err != nil {
			log.Errorf("Cannot convert %v to the desired type for %v: %v", defaultObj, destination, err)
			return false, nil, nil
		}
		// Convert from unstructured to typed sometimes drops the gvk, re-add it here.
		tObj.GetObjectKind().SetGroupVersionKind(uObj.GetObjectKind().GroupVersionKind())
	}
	switch newAction := action.DeepCopy().(type) {
	case clienttesting.CreateActionImpl:
		newAction.Object = tObj
		generateNameOnCreate(tObj)
		generateNameOnCreate(action.(clienttesting.CreateActionImpl).Object)
		_, err := destination.Invokes(newAction, defaultObj)
		if err != nil {
			log.Errorf("Propagating Invoke resulted in error for fake %T: %s", destination, err)
		}
	case clienttesting.UpdateActionImpl:
		newAction.Object = tObj
		_, err := destination.Invokes(newAction, defaultObj)
		if err != nil {
			log.Errorf("Propagating Invoke resulted in error for fake %T: %s", destination, err)
		}
	default:
		_, err := destination.Invokes(newAction, defaultObj)
		if err != nil {
			log.Errorf("Propagating Invoke resulted in error for fake %T: %s", destination, err)
		}
	}
	fm.mergeLock.Lock()
	fm.inProgress.Delete(key)
	fm.mergeLock.Unlock()
	// even if others return handled=true, we still want this client to react.
	return false, nil, nil
}

func (fm *fakeMerger) Merge(f clienttesting.FakeClient) {
	fm.init()
	fm.mergeLock.Lock()
	defer fm.mergeLock.Unlock()
	fm.alertList = append(fm.alertList, f)
	myPropagator := func(action clienttesting.Action) (bool, runtime.Object, error) {
		// propagate from typed client to dynamic
		return fm.propagateReactionSingle(fm.dynamic, action)
	}
	f.PrependReactor("*", "*", myPropagator)
}

func (fm *fakeMerger) MergeDynamic(df *dynamicfake.FakeDynamicClient) {
	fm.dynamic = df
	myPropagator := func(action clienttesting.Action) (bool, runtime.Object, error) {
		for _, f := range fm.alertList {
			// propagate from dynamic to any typed client that cares.
			_, _, _ = fm.propagateReactionSingle(f, action)
		}
		// even if others return handled=true, we still want this client to react.
		return false, nil, nil
	}
	df.PrependReactor("*", "*", myPropagator)
}

type objectiveAction interface {
	GetObject() runtime.Object
}

type fieldManagerFactory struct {
	schema    *runtime.Scheme
	existing  map[schema.GroupVersionKind]*managedfields.FieldManager
	converter managedfields.TypeConverter
	mu        sync.Mutex
}

func newFieldManagerFacotry(myschema *runtime.Scheme) *fieldManagerFactory {
	return &fieldManagerFactory{
		schema:    myschema,
		converter: managedfields.NewDeducedTypeConverter(),
		existing:  make(map[schema.GroupVersionKind]*managedfields.FieldManager),
	}
}

func (fmf *fieldManagerFactory) FieldManager(gvk schema.GroupVersionKind) *managedfields.FieldManager {
	fmf.mu.Lock()
	defer fmf.mu.Unlock()
	if result, ok := fmf.existing[gvk]; ok {
		return result
	}
	result, err := managedfields.NewDefaultFieldManager(
		fmf.converter, fmf.schema, fmf.schema, fmf.schema,
		gvk, gvk.GroupVersion(), "", make(map[fieldpath.APIVersion]*fieldpath.Set))
	if err != nil {
		panic(err)
	}
	fmf.existing[gvk] = result
	return result
}

func generateNameOnCreate(ret runtime.Object) {
	// https://github.com/kubernetes/client-go/issues/439
	meta, ok := ret.(metav1.Object)
	if !ok {
		return
	}

	if meta.GetName() == "" && meta.GetGenerateName() != "" {
		meta.SetName(names.SimpleNameGenerator.GenerateName(meta.GetGenerateName()))
	}
}
