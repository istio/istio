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

	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// registerHandlerAsBatched is a helper to register the provided handler as a batched handler. This allows collections to
// only implement RegisterBatch.
func registerHandlerAsBatched[T any](c Collection[T], f func(o Event[T])) {
	c.RegisterBatch(func(events []Event[T]) {
		for _, o := range events {
			f(o)
		}
	})
}

// erasedCollection is a Collection[T] that has been type-erased so it can be stored in collections
// that do not have type information.
type erasedCollection struct {
	// original stores the original typed Collection
	original any
	// registerFunc registers any Event[any] handler. These will be mapped to Event[T] when connected to the original collection.
	registerFunc func(f func(o []Event[any]))
	name         string
}

func (e erasedCollection) register(f func(o []Event[any])) {
	e.registerFunc(f)
}

func eraseCollection[T any](c Collection[T]) erasedCollection {
	return erasedCollection{
		name:     c.Name(),
		original: c,
		registerFunc: func(f func(o []Event[any])) {
			c.RegisterBatch(func(o []Event[T]) {
				f(slices.Map(o, castEvent[T, any]))
			})
		},
	}
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
	return *c
}

// collectionOptions tracks options for a collection
type collectionOptions struct {
	name         string
	augmentation func(o any) any
}

// dependency is a specific thing that can be depended on
type dependency struct {
	// The actual collection containing this
	collection erasedCollection
	// Filter over the collection
	filter filter
}

// registerDependency is an internal interface for things that can register dependencies.
// This is called from Fetch to Collections, generally.
type registerDependency interface {
	// Registers a dependency, returning true if it is finalized
	registerDependency(dependency)
	Name() string
}

type augmenter interface {
	augment(any) any
}

func objectOrAugmented[T any](c Collection[T], o any) any {
	if a, ok := c.(augmenter); ok {
		return a.augment(o)
	}
	return o
}

// getName returns the name for an object, of possible.
// Warning: this will panic if the name is not available.
func getName(a any) string {
	ak, ok := a.(Namer)
	if ok {
		return ak.GetName()
	}
	panic(fmt.Sprintf("No Name, got %T %+v", a, a))
	return ""
}

// tryGetKey returns the Key for an object. If not possible, returns false
func tryGetKey[O any](a O) (Key[O], bool) {
	as, ok := any(a).(string)
	if ok {
		return Key[O](as), true
	}
	ao, ok := any(a).(controllers.Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return Key[O](k), true
	}
	ac, ok := any(a).(config.Config)
	if ok {
		return Key[O](keyFunc(ac.Name, ac.Namespace)), true
	}
	arn, ok := any(a).(ResourceNamer)
	if ok {
		return Key[O](arn.ResourceName()), true
	}
	ack := GetApplyConfigKey(a)
	if ack != nil {
		return *ack, true
	}
	return "", false
}

// getNamespace returns the namespace for an object, of possible.
// Warning: this will panic if the namespace is not available.
func getNamespace(a any) string {
	ak, ok := a.(Namespacer)
	if ok {
		return ak.GetNamespace()
	}
	panic(fmt.Sprintf("No Namespace, got %T", a))
	return ""
}

// getLabels returns the labels for an object, of possible.
// Warning: this will panic if the labels is not available.
func getLabels(a any) map[string]string {
	al, ok := a.(labeler)
	if ok {
		return al.GetLabels()
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
	return nil
}

// getLabelSelector returns the labels for an object, of possible.
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
		return nil
	}

	labelsField := specField.FieldByName("Selector")
	if !labelsField.IsValid() {
		panic(fmt.Sprintf("obj %T has no Selector", a))
		return nil
	}

	switch s := labelsField.Interface().(type) {
	case *v1beta1.WorkloadSelector:
		return s.GetMatchLabels()
	case map[string]string:
		return s
	default:
		panic(fmt.Sprintf("obj %T has unknown Selector", s))
		return nil
	}
}

// equal checks if two objects are equal. This is done through a variety of different methods, depending on the input type.
func equal[O any](a, b O) bool {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Equals(b)
	}
	// TODO: this is probably not safe. If we are comparing objects from the API server its fine,
	// but often we will be operating on types generated by the controller itself.
	// We should have a way to opt-in to RV comparison, but not default to it.
	//ao, ok := any(a).(controllers.Object)
	//if ok {
	//	return ao.GetResourceVersion() == any(b).(controllers.Object).GetResourceVersion()
	//}
	ap, ok := any(a).(proto.Message)
	if ok {
		if reflect.TypeOf(ap.ProtoReflect().Interface()) == reflect.TypeOf(ap) {
			return proto.Equal(ap, any(b).(proto.Message))
		}
		// If not, this is an embedded proto! Sneaky.
		// TODO: panic? I don't think reflect.DeepEqual on proto is good
	}
	return reflect.DeepEqual(a, b)
}
