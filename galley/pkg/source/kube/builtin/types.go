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

package builtin

import (
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"

	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/source/kube/stats"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	types = map[string]*Type{
		"Service": {
			spec:    getSpec("Service"),
			isEqual: resourceVersionsMatch,
			extractObject: func(o interface{}) metav1.Object {
				if obj, ok := o.(metav1.Object); ok {
					return obj
				}
				return nil
			},
			extractResource: func(o interface{}) proto.Message {
				if obj, ok := o.(*v1.Service); ok {
					return &obj.Spec
				}
				return nil
			},
			newInformer: func(sharedInformers informers.SharedInformerFactory) cache.SharedIndexInformer {
				return sharedInformers.Core().V1().Services().Informer()
			},
			parseJSON: func(input []byte) (interface{}, error) {
				out := &v1.Service{}
				if _, _, err := deserializer.Decode(input, nil, out); err != nil {
					return nil, err
				}
				return out, nil
			},
		},
		"Node": {
			spec:    getSpec("Node"),
			isEqual: resourceVersionsMatch,
			extractObject: func(o interface{}) metav1.Object {
				if obj, ok := o.(metav1.Object); ok {
					return obj
				}
				return nil
			},
			extractResource: func(o interface{}) proto.Message {
				if obj, ok := o.(*v1.Node); ok {
					return &obj.Spec
				}
				return nil
			},
			newInformer: func(sharedInformers informers.SharedInformerFactory) cache.SharedIndexInformer {
				return sharedInformers.Core().V1().Nodes().Informer()
			},
			parseJSON: func(input []byte) (interface{}, error) {
				out := &v1.Node{}
				if _, _, err := deserializer.Decode(input, nil, out); err != nil {
					return nil, err
				}
				return out, nil
			},
		},
		"Pod": {
			spec:    getSpec("Pod"),
			isEqual: resourceVersionsMatch,
			extractObject: func(o interface{}) metav1.Object {
				if obj, ok := o.(metav1.Object); ok {
					return obj
				}
				return nil
			},
			extractResource: func(o interface{}) proto.Message {
				if obj, ok := o.(*v1.Pod); ok {
					return obj
				}
				return nil
			},
			newInformer: func(sharedInformers informers.SharedInformerFactory) cache.SharedIndexInformer {
				return sharedInformers.Core().V1().Pods().Informer()
			},
			parseJSON: func(input []byte) (interface{}, error) {
				out := &v1.Pod{}
				if _, _, err := deserializer.Decode(input, nil, out); err != nil {
					return nil, err
				}
				return out, nil
			},
		},
		"Endpoints": {
			spec: getSpec("Endpoints"),
			isEqual: func(o1 interface{}, o2 interface{}) bool {
				r1, ok1 := o1.(*v1.Endpoints)
				r2, ok2 := o2.(*v1.Endpoints)
				if !ok1 || !ok2 {
					msg := fmt.Sprintf("error decoding kube endpoints during update, o1 type: %v, o2 type: %v",
						reflect.TypeOf(o1),
						reflect.TypeOf(o2))
					log.Scope.Error(msg)
					stats.RecordEventError(msg)
					return false
				}
				// Endpoint updates can be noisy. Make sure that the subsets have actually changed.
				return reflect.DeepEqual(r1.Subsets, r2.Subsets)
			},
			extractObject: func(o interface{}) metav1.Object {
				if obj, ok := o.(metav1.Object); ok {
					return obj
				}
				return nil
			},
			extractResource: func(o interface{}) proto.Message {
				// TODO(nmittler): This copies ObjectMeta since Endpoints have no spec.
				if obj, ok := o.(*v1.Endpoints); ok {
					return obj
				}
				return nil
			},
			newInformer: func(sharedInformers informers.SharedInformerFactory) cache.SharedIndexInformer {
				return sharedInformers.Core().V1().Endpoints().Informer()
			},
			parseJSON: func(input []byte) (interface{}, error) {
				out := &v1.Endpoints{}
				if _, _, err := deserializer.Decode(input, nil, out); err != nil {
					return nil, err
				}
				return out, nil
			},
		},
	}
)

func getSpec(kind string) *schema.ResourceSpec {
	spec := kubeMeta.Types.Get(kind)
	if spec == nil {
		panic("spec undefined: " + kind)
	}
	return spec
}

// IsBuiltIn indicates whether there is a built-in source for the given kind.
func IsBuiltIn(kind string) bool {
	_, ok := types[kind]
	return ok
}

// GetType returns the built-in Type for the given kind, or nil if the kind is unsupported.
func GetType(kind string) *Type {
	return types[kind]
}

// GetSchema returns the schema for all built-in types.
func GetSchema() *schema.Instance {
	b := schema.NewBuilder()
	for _, p := range types {
		b.Add(*p.GetSpec())
	}
	return b.Build()
}

// resourceVersionsMatch is a resourceEqualFn that determines equality by the resource version.
func resourceVersionsMatch(o1 interface{}, o2 interface{}) bool {
	r1, ok1 := o1.(metav1.Object)
	r2, ok2 := o2.(metav1.Object)
	if !ok1 || !ok2 {
		msg := fmt.Sprintf("error decoding kube objects during update, o1 type: %v, o2 type: %v",
			reflect.TypeOf(o1),
			reflect.TypeOf(o2))
		log.Scope.Error(msg)
		stats.RecordEventError(msg)
		return false
	}
	return r1.GetResourceVersion() == r2.GetResourceVersion()
}
