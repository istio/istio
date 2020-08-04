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

package data

import (
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

var (
	// K8SCollection2 is a testing collection
	K8SCollection2 = basicmeta.MustGet2().KubeCollections().MustFind("k8s/collection2")

	Foo = collection.Builder{
		Name:         "foo",
		VariableName: "Foo",
		Resource: resource.Builder{
			Kind:         "Foo",
			Plural:       "Foos",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()

	Bar = collection.Builder{
		Name:         "bar",
		VariableName: "Bar",
		Resource: resource.Builder{
			Kind:         "Bar",
			Plural:       "Bars",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()

	Boo = collection.Builder{
		Name:         "boo",
		VariableName: "Boo",
		Resource: resource.Builder{
			Kind:         "Boo",
			Plural:       "Boos",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()

	Baz = collection.Builder{
		Name:         "baz",
		VariableName: "Baz",
		Resource: resource.Builder{
			Kind:         "Baz",
			Plural:       "Bazes",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()
)
