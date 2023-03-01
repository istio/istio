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

package codegen

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/test/util/assert"
)

func TestStaticCollections(t *testing.T) {
	cases := []struct {
		packageName string
		m           *ast.Metadata
		err         string
		output      string
	}{
		{
			packageName: "pkg",
			m: &ast.Metadata{
				Resources: []*ast.Resource{
					{
						Identifier:    "Foo",
						Group:         "foo.group",
						Version:       "v1",
						Kind:          "fookind",
						Plural:        "fookinds",
						ClusterScoped: true,
						Builtin:       true,
						Proto:         "google.protobuf.Struct",
						ProtoPackage:  "github.com/gogo/protobuf/types",
						Validate:      "EmptyValidate",
					},
					{
						Identifier:    "Bar",
						Group:         "bar.group",
						Version:       "v1",
						Kind:          "barkind",
						Plural:        "barkinds",
						ClusterScoped: false,
						Proto:         "google.protobuf.Struct",
						ProtoPackage:  "github.com/gogo/protobuf/types",
						Validate:      "EmptyValidate",
					},
				},
			},
			output: `
// GENERATED FILE -- DO NOT EDIT
//

package pkg

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
    "reflect"
	githubcomgogoprotobuftypes "github.com/gogo/protobuf/types"
)

var (

	Bar = resource.Builder {
			Identifier: "Bar",
			Group: "bar.group",
			Kind: "barkind",
			Plural: "barkinds",
			Version: "v1",
			Proto: "google.protobuf.Struct",
			ReflectType: reflect.TypeOf(&githubcomgogoprotobuftypes.Struct{}).Elem(),
			ProtoPackage: "github.com/gogo/protobuf/types",
			ClusterScoped: false,
			Builtin: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild()

	Foo = resource.Builder {
			Identifier: "Foo",
			Group: "foo.group",
			Kind: "fookind",
			Plural: "fookinds",
			Version: "v1",
			Proto: "google.protobuf.Struct",
			ReflectType: reflect.TypeOf(&githubcomgogoprotobuftypes.Struct{}).Elem(),
			ProtoPackage: "github.com/gogo/protobuf/types",
			ClusterScoped: true,
			Builtin: true,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild()


	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
		MustAdd(Bar).
		MustAdd(Foo).
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
		MustAdd(Foo).
		Build()

	// Pilot contains only collections used by Pilot.
	Pilot = collection.NewSchemasBuilder().
		Build()

	// PilotGatewayAPI contains only collections used by Pilot, including experimental Service Api.
	PilotGatewayAPI = collection.NewSchemasBuilder().
		Build()
)
`,
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewWithT(t)

			s, err := StaticCollections(c.packageName, c.m, func(name string) bool {
				return true
			}, "")
			if c.err != "" {
				g.Expect(err).NotTo(BeNil())
				g.Expect(err.Error()).To(Equal(s))
			} else {
				g.Expect(err).To(BeNil())
				fmt.Println(strings.TrimSpace(c.output))
				assert.Equal(t, strings.TrimSpace(s), strings.TrimSpace(c.output))
			}
		})
	}
}
