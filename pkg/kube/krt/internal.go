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

package krt

import (
	"fmt"
	"reflect"

	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

// registerHandlerAsBatched is a helper to register the provided handler as a batched handler. This allows collections to
// only implement RegisterBatch.
func registerHandlerAsBatched[T any](c internalCollection[T], f func(o Event[T])) Syncer {
	return c.RegisterBatch(func(events []Event[T], initialSync bool) {
		for _, o := range events {
			f(o)
		}
	}, true)
}

// castEvent converts an Event[I] to Event[O].
// Caller is responsible for making sure these can be type converted.
// Typically this is converting to or from `any`.
func castEvent[I, O any](o Event[I]) Event[O] {
	e := Event[O]{
		Event: o.Event,
	}
	if o.Old != nil {
		e.Old = ptr.Of(any(*o.Old).(O))
	}
	if o.New != nil {
		e.New = ptr.Of(any(*o.New).(O))
	}
	return e
}

func buildCollectionOptions(opts ...CollectionOption) collectionOptions {
	c := &collectionOptions{}
	for _, o := range opts {
		o(c)
	}
	if c.stop == nil {
		c.stop = make(chan struct{})
	}
	return *c
}

// collectionOptions tracks options for a collection
type collectionOptions struct {
	name         string
	augmentation func(o any) any
	stop         <-chan struct{}
	debugger     *DebugHandler
}

type indexedDependency struct {
	id  collectionUID
	key string
}

// dependency is a specific thing that can be depended on
type dependency struct {
	id             collectionUID
	collectionName string
	// Filter over the collection
	filter *filter
}

type erasedEventHandler = func(o []Event[any], initialSync bool)

// registerDependency is an internal interface for things that can register dependencies.
// This is called from Fetch to Collections, generally.
type registerDependency interface {
	// Registers a dependency, returning true if it is finalized
	registerDependency(*dependency, Syncer, func(f erasedEventHandler) Syncer)
	name() string
}

// tryGetKey returns the Key for an object. If not possible, returns false
func tryGetKey[O any](a O) (string, bool) {
	as, ok := any(a).(string)
	if ok {
		return as, true
	}
	ao, ok := any(a).(controllers.Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return k, true
	}
	ac, ok := any(a).(config.Config)
	if ok {
		return keyFunc(ac.Name, ac.Namespace), true
	}
	arn, ok := any(a).(ResourceNamer)
	if ok {
		return arn.ResourceName(), true
	}
	ack := GetApplyConfigKey(a)
	if ack != nil {
		return *ack, true
	}
	return "", false
}

// getLabels returns the labels for an object, if possible.
// Warning: this will panic if the labels is not available.
func getLabels(a any) map[string]string {
	al, ok := a.(Labeler)
	if ok {
		return al.GetLabels()
	}
	pal, ok := any(&a).(Labeler)
	if ok {
		return pal.GetLabels()
	}
	ak, ok := a.(metav1.Object)
	if ok {
		return ak.GetLabels()
	}
	ac, ok := a.(config.Config)
	if ok {
		return ac.Labels
	}
	panic(fmt.Sprintf("No Labels, got %T", a))
}

// getLabelSelector returns the labels for an object, if possible.
// Warning: this will panic if the labelSelectors is not available.
func getLabelSelector(a any) map[string]string {
	ak, ok := a.(LabelSelectorer)
	if ok {
		return ak.GetLabelSelector()
	}
	val := reflect.ValueOf(a)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	specField := val.FieldByName("Spec")
	if !specField.IsValid() {
		panic(fmt.Sprintf("obj %T has no Spec", a))
	}

	labelsField := specField.FieldByName("Selector")
	if !labelsField.IsValid() {
		panic(fmt.Sprintf("obj %T has no Selector", a))
	}

	switch s := labelsField.Interface().(type) {
	case *v1beta1.WorkloadSelector:
		return s.GetMatchLabels()
	case map[string]string:
		return s
	default:
		panic(fmt.Sprintf("obj %T has unknown Selector", s))
	}
}

// equal checks if two objects are equal. This is done through a variety of different methods, depending on the input type.
func equal[O any](a, b O) bool {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Equals(b)
	}
	pk, ok := any(&a).(Equaler[O])
	if ok {
		return pk.Equals(b)
	}
	// Future improvement: add a default Kubernetes object implementation
	// ResourceVersion is tempting but probably not safe. If we are comparing objects from the API server its fine,
	// but often we will be operating on types generated by the controller itself.
	// We should have a way to opt-in to RV comparison, but not default to it.

	ap, ok := any(a).(proto.Message)
	if ok {
		if reflect.TypeOf(ap.ProtoReflect().Interface()) == reflect.TypeOf(ap) {
			return proto.Equal(ap, any(b).(proto.Message))
		}
		// If not, this is an embedded proto most likely... Sneaky.
		// DeepEqual on proto is broken, so fail fast to avoid subtle errors.
		panic(fmt.Sprintf("unable to compare object %T; perhaps it is embedding a protobuf? Provide an Equaler implementation", a))
	}
	return reflect.DeepEqual(a, b)
}

type collectionUID uint64

var globalUIDCounter = atomic.NewUint64(1)

func nextUID() collectionUID {
	return collectionUID(globalUIDCounter.Inc())
}
