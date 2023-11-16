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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	acmetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
)

func Filter[T any](data []T, f func(T) bool) []T {
	fltd := make([]T, 0, len(data))
	for _, e := range data {
		if f(e) {
			fltd = append(fltd, e)
		}
	}
	return fltd
}

func GetKey[O any](a O) Key[O] {
	if k, ok := tryGetKey[O](a); ok {
		return k
	}
	// Allow pointer receiver as well
	if k, ok := tryGetKey[*O](&a); ok {
		return Key[O](k)
	}
	panic(fmt.Sprintf("Cannot get Key, got %T", a))
	return ""
}

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
		return Key[O](KeyFunc(ac.Name, ac.Namespace)), true
	}
	arn, ok := any(a).(resourceNamer)
	if ok {
		return Key[O](arn.ResourceName()), true
	}
	ack := GetApplyConfigKey(a)
	if ack != nil {
		return *ack, true
	}
	return "", false
}

func AppendNonNil[T any](data []T, i *T) []T {
	if i != nil {
		data = append(data, *i)
	}
	return data
}

func HasName(a any) bool {
	_, ok := a.(controllers.Object)
	return ok
}

type Named struct {
	Name, Namespace string
}

func NewNamed(o metav1.ObjectMeta) Named {
	return Named{Name: o.GetName(), Namespace: o.GetNamespace()}
}

func (n Named) ResourceName() string {
	return n.Namespace + "/" + n.Name
}

func (n Named) GetName() string {
	return n.Name
}

func (n Named) GetNamespace() string {
	return n.Namespace
}

type selector interface {
	GetLabelSelector() map[string]string
}

type namer interface {
	GetName() string
}

type namespacer interface {
	GetNamespace() string
}

type labeler interface {
	GetLabels() map[string]string
}

func GetName(a any) string {
	ak, ok := a.(namer)
	if ok {
		return ak.GetName()
	}
	log.Debugf("No Name, got %T", a)
	panic(fmt.Sprintf("No Name, got %T %+v", a, a))
	return ""
}

func GetNamespace(a any) string {
	ak, ok := a.(namespacer)
	if ok {
		return ak.GetNamespace()
	}
	panic(fmt.Sprintf("No Namespace, got %T", a))
	return ""
}

func GetLabels(a any) map[string]string {
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

func GetApplyConfigKey[O any](a O) *Key[O] {
	val := reflect.ValueOf(a)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	specField := val.FieldByName("ObjectMetaApplyConfiguration")
	if !specField.IsValid() {
		return nil
	}
	meta := specField.Interface().(*acmetav1.ObjectMetaApplyConfiguration)
	if meta.Namespace != nil && len(*meta.Namespace) > 0 {
		return ptr.Of(Key[O](*meta.Namespace + "/" + *meta.Name))
	}
	return ptr.Of(Key[O](*meta.Name))
}

func GetLabelSelector(a any) map[string]string {
	ak, ok := a.(selector)
	if ok {
		return ak.GetLabelSelector()
	}
	val := reflect.ValueOf(a)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	specField := val.FieldByName("Spec")
	if !specField.IsValid() {
		log.Debugf("obj %T has no Spec", a)
		return nil
	}

	labelsField := specField.FieldByName("Selector")
	if !labelsField.IsValid() {
		log.Debugf("obj %T has no Selector", a)
		return nil
	}

	switch s := labelsField.Interface().(type) {
	case *v1beta1.WorkloadSelector:
		return s.GetMatchLabels()
	case map[string]string:
		return s
	default:
		log.Debugf("obj %T has unknown Selector", s)
		return nil
	}
}

func GetType[T any]() reflect.Type {
	t := reflect.TypeOf(*new(T))
	if t.Kind() != reflect.Struct {
		return t.Elem()
	}
	return t
}

func GetTypeOf(a any) reflect.Type {
	t := reflect.TypeOf(a)
	if t.Kind() != reflect.Struct {
		return t.Elem()
	}
	return t
}

// KeyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func KeyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}

func SplitKeyFunc(key string) (namespace, name string) {
	parts := strings.Split(key, "/")
	switch len(parts) {
	case 1:
		// name only, no namespace
		return "", parts[0]
	case 2:
		// namespace and name
		return parts[0], parts[1]
	}

	return "", ""
}
