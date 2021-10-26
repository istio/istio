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
package local

// Test helpers common to this package

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	githubcomgogoprotobuftypes "github.com/gogo/protobuf/types"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	r2 "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
)

// K8SCollection1 describes the collection k8s/collection1
var K8SCollection1 = collection.Builder{
	Name:         "k8s/collection1",
	VariableName: "K8SCollection1",
	Disabled:     false,
	Resource: r2.Builder{
	Group:         "testdata.istio.io",
	Kind:          "Kind1",
	Plural:        "Kind1s",
	Version:       "v1alpha1",
	Proto:         "google.protobuf.Struct",
	ReflectType:   reflect.TypeOf(&githubcomgogoprotobuftypes.Struct{}).Elem(),
	ProtoPackage:  "github.com/gogo/protobuf/types",
	ClusterScoped: false,
	ValidateProto: validation.EmptyValidate,
}.MustBuild(),
}.MustBuild()

// Collection2 describes the collection collection2
var Collection2 = collection.Builder{
	Name:         "collection2",
	VariableName: "Collection2",
	Disabled:     false,
	Resource: r2.Builder{
		Group:         "testdata.istio.io",
		Kind:          "Kind1",
		Plural:        "Kind1s",
		Version:       "v1alpha1",
		Proto:         "google.protobuf.Struct",
		ReflectType:   reflect.TypeOf(&githubcomgogoprotobuftypes.Struct{}).Elem(),
		ProtoPackage:  "github.com/gogo/protobuf/types",
		ClusterScoped: false,
		ValidateProto: validation.EmptyValidate,
	}.MustBuild(),
}.MustBuild()

func createTestEvent(t *testing.T, k event.Kind, r *resource.Instance) event.Event {
	t.Helper()
	return event.Event{
		Kind:     k,
		Source:   K8SCollection1,
		Resource: r,
	}
}

func createTestResource(t *testing.T, ns, name, version string) *resource.Instance {
	t.Helper()
	rname := resource.NewFullName(resource.Namespace(ns), resource.LocalName(name))
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: rname,
			Version:  resource.Version(version),
		},
		Message: &types.Empty{},
		Origin: &kube.Origin{
			FullName: rname,
		},
	}
}
