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
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/stats"
	"istio.io/istio/pkg/config/resource"
)

// Adapter provides core functions that are necessary to interact with a Kubernetes resource.
type Adapter struct {
	extractObject                 extractObjectFn
	extractResource               extractResourceFn
	newInformer                   newInformerFn
	parseJSON                     parseJSONFn
	getStatus                     getStatusFn
	isEqual                       isEqualFn
	isBuiltIn                     bool
	isDefaultExcluded             bool
	isRequiredForServiceDiscovery bool
}

// ExtractObject extracts the k8s object metadata from the given object of this type.
func (p *Adapter) ExtractObject(o interface{}) metav1.Object {
	return p.extractObject(o)
}

// ExtractResource extracts the resource proto from the given object of this type.
func (p *Adapter) ExtractResource(o interface{}) (proto.Message, error) {
	return p.extractResource(o)
}

// NewInformer creates a new k8s informer for resources of this type.
func (p *Adapter) NewInformer() (cache.SharedInformer, error) {
	return p.newInformer()
}

// ParseJSON parses the given JSON into a k8s object of this type.
func (p *Adapter) ParseJSON(input []byte) (interface{}, error) {
	return p.parseJSON(input)
}

// GetStatus returns the status of the resource.
func (p *Adapter) GetStatus(o interface{}) interface{} {
	return p.getStatus(o)
}

// IsEqual checks whether the given two resources are equal
func (p *Adapter) IsEqual(o1, o2 interface{}) bool {
	return p.isEqual(o1, o2)
}

// IsBuiltIn returns true if the adapter uses built-in client libraries.
func (p *Adapter) IsBuiltIn() bool {
	return p.isBuiltIn
}

// IsDefaultExcluded returns true if the adapter is excluded from the default set of resources to watch.
func (p *Adapter) IsDefaultExcluded() bool {
	return p.isDefaultExcluded
}

// IsRequiredForServiceDiscovery returns true if the adapter is required for service discovery.
func (p *Adapter) IsRequiredForServiceDiscovery() bool {
	return p.isRequiredForServiceDiscovery
}

// JSONToEntry parses the K8s Resource in JSON form and converts it to resource entry.
func (p *Adapter) JSONToEntry(s string) (*resource.Instance, error) {
	i, err := p.ParseJSON([]byte(s))
	if err != nil {
		return nil, err
	}

	obj := p.ExtractObject(i)
	item, err := p.ExtractResource(i)
	if err != nil {
		return nil, err
	}

	return ToResource(obj, nil, item, nil), nil

}

type extractObjectFn func(o interface{}) metav1.Object
type extractResourceFn func(o interface{}) (proto.Message, error)
type newInformerFn func() (cache.SharedIndexInformer, error)
type parseJSONFn func(input []byte) (interface{}, error)
type getStatusFn func(o interface{}) interface{}
type isEqualFn func(o1 interface{}, o2 interface{}) bool

// resourceVersionsMatch is a resourceEqualFn that determines equality by the resource version.
func resourceVersionsMatch(o1 interface{}, o2 interface{}) bool {
	r1, ok1 := o1.(metav1.Object)
	r2, ok2 := o2.(metav1.Object)
	if !ok1 || !ok2 {
		msg := fmt.Sprintf("error decoding kube objects during update, o1 type: %v, o2 type: %v",
			reflect.TypeOf(o1),
			reflect.TypeOf(o2))
		scope.Source.Error(msg)
		stats.RecordEventError(msg)
		return false
	}
	return r1.GetResourceVersion() == r2.GetResourceVersion()
}
