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
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/galley/pkg/source/kube/schema"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Type provides functions for a single built-in type.
type Type struct {
	spec            *schema.ResourceSpec
	newInformer     newInformerFn
	isEqual         resourceEqualFn
	extractObject   objectExtractFn
	extractResource itemExtractFn
	parseJSON       parseJSONFn
}

// GetSpec returns the schema.ResourceSpec associated with this built-in type.
func (p *Type) GetSpec() *schema.ResourceSpec {
	return p.spec
}

// IsEqual indicates if the two objects of this type are equivalent
func (p *Type) IsEqual(o1 interface{}, o2 interface{}) bool {
	return p.isEqual(o1, o2)
}

// ExtractObject extracts the k8s object metadata from the given object of this type.
func (p *Type) ExtractObject(o interface{}) metav1.Object {
	return p.extractObject(o)
}

// ExtractResource extracts the resource proto from the given object of this type.
func (p *Type) ExtractResource(o interface{}) proto.Message {
	return p.extractResource(o)
}

// NewInformer creates a new k8s informer for resources of this type.
func (p *Type) NewInformer(sharedInformers informers.SharedInformerFactory) cache.SharedIndexInformer {
	return p.newInformer(sharedInformers)
}

// ParseJSON parses the given JSON into a k8s object of this type.
func (p *Type) ParseJSON(input []byte) (interface{}, error) {
	return p.parseJSON(input)
}

type resourceEqualFn func(o1 interface{}, o2 interface{}) bool
type objectExtractFn func(o interface{}) metav1.Object
type itemExtractFn func(o interface{}) proto.Message
type newInformerFn func(sharedInformers informers.SharedInformerFactory) cache.SharedIndexInformer
type parseJSONFn func(input []byte) (interface{}, error)
