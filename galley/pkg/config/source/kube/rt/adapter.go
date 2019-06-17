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
	"github.com/gogo/protobuf/proto"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	// 	"istio.io/istio/galley/pkg/config/schema"
)

// Adapter provides core functions that are necessary to interact with a Kubernetes resource.
type Adapter struct {
	extractObject   extractObjectFn
	extractResource extractResourceFn
	newInformer     newInformerFn
	parseJSON       parseJSONFn
	isBuiltIn       bool
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
func (p *Adapter) NewInformer() (cache.SharedIndexInformer, error) {
	return p.newInformer()
}

// ParseJSON parses the given JSON into a k8s object of this type.
func (p *Adapter) ParseJSON(input []byte) (interface{}, error) {
	return p.parseJSON(input)
}

// IsBuiltIn returns true if the adapter uses built-in client libraries.
func (p *Adapter) IsBuiltIn() bool {
	return p.isBuiltIn
}

type extractObjectFn func(o interface{}) metav1.Object
type extractResourceFn func(o interface{}) (proto.Message, error)
type newInformerFn func() (cache.SharedIndexInformer, error)
type parseJSONFn func(input []byte) (interface{}, error)
