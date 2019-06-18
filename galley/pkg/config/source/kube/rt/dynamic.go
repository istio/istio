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

package rt

import (
	"encoding/json"
	"fmt"

	"github.com/gogo/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/config/schema"
)

func (p *Provider) getDynamicAdapter(r schema.KubeResource) *Adapter {
	return &Adapter{
		extractObject: func(o interface{}) metav1.Object {
			r, ok := o.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			return r
		},

		extractResource: func(o interface{}) (proto.Message, error) {
			u, ok := o.(*unstructured.Unstructured)
			if !ok {
				return nil, fmt.Errorf("extractResourc: not unstructured: %v", o)
			}

			pr := r.Collection.NewProtoInstance()
			if err := unmarshalProto(pr, u.Object["spec"]); err != nil {
				return nil, err
			}

			return pr, nil
		},

		newInformer: func() (cache.SharedIndexInformer, error) {
			d, err := p.dynamicResource(r)
			if err != nil {
				return nil, err
			}

			return cache.NewSharedIndexInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return d.List(options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						options.Watch = true
						return d.Watch(options)
					},
				},
				&unstructured.Unstructured{},
				p.resyncPeriod,
				cache.Indexers{}), nil
		},

		parseJSON: func(data []byte) (interface{}, error) {
			u := &unstructured.Unstructured{}
			if err := json.Unmarshal(data, u); err != nil {
				return nil, fmt.Errorf("failed marshaling into unstructured: %v", err)
			}

			if empty(u) {
				return nil, nil
			}

			return u, nil
		},
		isEqual: resourceVersionsMatch,
		isBuiltIn: false,
	}
}

// Check if the parsed resource is empty
func empty(r *unstructured.Unstructured) bool {
	if r.Object == nil || len(r.Object) == 0 {
		return true
	}
	return false
}
