// Copyright 2017 Istio Authors
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

package crd

import (
	"bytes"
	"encoding/json"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//resource type represents the structure of a single custom resource.
type resource struct {
	Kind              string `json:"kind"`
	APIVersion        string `json:"apiVersion"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec map[string]interface{} `json:"spec,omitempty"`
}

func deepCopy(s interface{}) interface{} {
	switch x := s.(type) {
	case map[string]interface{}:
		clone := make(map[string]interface{}, len(x))
		for k, v := range x {
			clone[k] = deepCopy(v)
		}
		return clone
	case []interface{}:
		clone := make([]interface{}, len(x))
		for i, v := range x {
			clone[i] = deepCopy(v)
		}
		return clone
	default:
		return x
	}
}

func deepCopySpec(s1 map[string]interface{}, s2 map[string]interface{}) {
	for k, v := range s1 {
		s2[k] = deepCopy(v)
	}
}

// GetObjectKind implements runtime.Object interface.
func (r *resource) GetObjectKind() schema.ObjectKind {
	return &metav1.TypeMeta{
		Kind:       r.Kind,
		APIVersion: apiVersion,
	}
}

// DeepCopyObject implements runtime.Object interface.
func (r *resource) DeepCopyObject() runtime.Object {
	r2 := &resource{Kind: r.Kind}
	r.ObjectMeta.DeepCopyInto(&r2.ObjectMeta)
	deepCopySpec(r.Spec, r2.Spec)
	return r2
}

// resourceList represents the data of listing custom resources.
type resourceList struct {
	Kind            string
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*resource `json:"items"`
}

// GetObjectKind implements runtime.Object interface.
func (r *resourceList) GetObjectKind() schema.ObjectKind {
	return &metav1.TypeMeta{
		Kind:       r.Kind,
		APIVersion: apiVersion,
	}
}

// GetObjectKind implements runtime.Object interface.
func (r *resourceList) DeepCopyObject() runtime.Object {
	r2 := &resourceList{
		ListMeta: *r.ListMeta.DeepCopy(),
		Items:    make([]*resource, len(r.Items)),
	}
	for i, item := range r.Items {
		r2.Items[i] = item.DeepCopyObject().(*resource)
	}
	return r2
}

func convert(spec map[string]interface{}, pbSpec proto.Message) error {
	// This is inefficient; convert to a protobuf message through JSON.
	// TODO: use reflect.
	jsonData, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	return jsonpb.Unmarshal(bytes.NewReader(jsonData), pbSpec)
}

func convertBack(pbSpec proto.Message, spec *map[string]interface{}) error {
	buf := bytes.NewBuffer(nil)
	if err := (&jsonpb.Marshaler{}).Marshal(buf, pbSpec); err != nil {
		return err
	}
	if err := json.Unmarshal(buf.Bytes(), spec); err != nil {
		return err
	}
	return nil
}
