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
	"istio.io/istio/pkg/typemap"
)

func MustGVRFromType[T runtime.Object]() schema.GroupVersionResource {
	if gk, ok := getGvk(ptr.Empty[T]()); ok {
		gr, ok := gvk.ToGVR(gk)
		if !ok {
			panic(fmt.Sprintf("unknown GVR for GVK %v", gk))
		}
		return gr
	}
	if rp := typemap.Get[RegisterType[T]](registeredTypes); rp != nil {
		return (*rp).GetGVR()
	}
	panic("unknown kind: " + ptr.TypeName[T]())
}

func GvrFromObject(t runtime.Object) schema.GroupVersionResource {
	gk := GvkFromObject(t)
	gr, ok := gvk.ToGVR(gk)
	if !ok {
		panic(fmt.Sprintf("unknown GVR for GVK %v", gk))
	}
	return gr
}

func MustGVKFromType[T runtime.Object]() (cfg config.GroupVersionKind) {
	if gvk, ok := getGvk(ptr.Empty[T]()); ok {
		return gvk
	}
	if rp := typemap.Get[RegisterType[T]](registeredTypes); rp != nil {
		return (*rp).GetGVK()
	}
	panic("unknown kind: " + cfg.String())
}

func MustToGVR[T runtime.Object](cfg config.GroupVersionKind) schema.GroupVersionResource {
	if r, ok := gvk.ToGVR(cfg); ok {
		return r
	}
	if rp := typemap.Get[RegisterType[T]](registeredTypes); rp != nil {
		return (*rp).GetGVR()
	}
	panic("unknown kind: " + cfg.String())
}

func GvkFromObject(obj runtime.Object) config.GroupVersionKind {
	if gvk, ok := getGvk(obj); ok {
		return gvk
	}
	panic("unknown kind: " + obj.GetObjectKind().GroupVersionKind().String())
}

var registeredTypes = typemap.NewTypeMap()

func Register[T runtime.Object](reg RegisterType[T]) {
	typemap.Set[RegisterType[T]](registeredTypes, reg)
}

type RegisterType[T runtime.Object] interface {
	GetGVK() config.GroupVersionKind
	GetGVR() schema.GroupVersionResource
	Object() T
}
