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

package rt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gogo/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/config/util/pb"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/listwatch"
)

func (p *Provider) getDynamicAdapter(r resource.Schema) *Adapter {
	return &Adapter{
		extractObject: func(o interface{}) metav1.Object {
			res, ok := o.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			return res
		},

		extractResource: func(o interface{}) (proto.Message, error) {
			u, ok := o.(*unstructured.Unstructured)
			if !ok {
				return nil, fmt.Errorf("extractResource: not unstructured: %v", o)
			}

			pr := r.MustNewProtoInstance()
			if err := pb.UnmarshalData(pr, u.Object["spec"]); err != nil {
				return nil, err
			}

			return pr, nil
		},

		newInformer: func() (cache.SharedIndexInformer, error) {
			d, err := p.GetDynamicResourceInterface(r)
			if err != nil {
				return nil, err
			}

			mlw := listwatch.MultiNamespaceListerWatcher(p.namespaces, func(namespace string) cache.ListerWatcher {
				return &cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return d.List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						options.Watch = true
						return d.Watch(context.TODO(), options)
					},
				}
			})

			informer := cache.NewSharedIndexInformer(mlw, &unstructured.Unstructured{}, p.resyncPeriod,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

			return informer, nil
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
		getStatus: func(o interface{}) interface{} {
			u, ok := o.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			return u.Object["status"]
		},
		isEqual:   resourceVersionsMatch,
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
