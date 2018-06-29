//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package converter

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/runtime/resource"
)

// Fn is a conversion function that converts the given unstructured CRD into the destination resource.
type Fn func(destination resource.Info, u *unstructured.Unstructured) (proto.Message, error)

var converters = map[string]Fn{
	"identity":           identity,
	"old-mixer-adapter":  mixerAdapter,
	"old-mixer-template": mixerTemplate,
}

// Get returns the named converter function, or panics if it is not found.
func Get(name string) Fn {
	fn, found := converters[name]
	if !found {
		panic(fmt.Sprintf("converter.Get: converter not found: %s", name))
	}

	return fn
}

func identity(destination resource.Info, u *unstructured.Unstructured) (proto.Message, error) {
	return toProto(destination, u.Object["spec"])
}

func mixerTemplate(destination resource.Info, u *unstructured.Unstructured) (proto.Message, error) {
	// TODO
	panic("mixer instance converter NYI")
}

func mixerAdapter(destination resource.Info, u *unstructured.Unstructured) (proto.Message, error) {
	// TODO
	panic("mixer instance converter NYI")
}
