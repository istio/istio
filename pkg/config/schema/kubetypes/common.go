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

package kubetypes

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
)

func GetGVR[T runtime.Object]() schema.GroupVersionResource {
	for _, reg := range registeredTypes {
		if m, ok := reg.(RegisterType[T]); ok {
			return m.GetGVR()
		}
	}
	gk := GetGVK[T]()
	gr, ok := gvk.ToGVR(gk)
	if !ok {
		panic(fmt.Sprintf("unknown GVR for GVK %v", gk))
	}
	return gr
}

func GetGVK[T runtime.Object]() (cfg config.GroupVersionKind) {
	for _, k := range registeredTypes {
		if t, ok := k.(RegisterType[T]); ok {
			return t.GetGVK()
		}
	}
	return getGvk(ptr.Empty[T]())
}

func GvkFromObject(obj runtime.Object) config.GroupVersionKind {
	return getGvk(obj)
}

var registeredTypes []any = make([]any, 0)

func Register[T runtime.Object](reg RegisterType[T]) {
	registeredTypes = append(registeredTypes, reg)
}

type RegisterType[T runtime.Object] interface {
	GetGVK() config.GroupVersionKind
	GetGVR() schema.GroupVersionResource
}
