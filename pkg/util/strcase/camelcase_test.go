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

package strcase_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/util/strcase"
)

func TestCamelCase(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"foo":           "Foo",
		"foobar":        "Foobar",
		"fooBar":        "FooBar",
		"foo_bar":       "FooBar",
		"foo-bar":       "FooBar",
		"foo_Bar":       "FooBar",
		"foo9bar":       "Foo9Bar",
		"HTTP-API-Spec": "HTTPAPISpec",
		"http-api-spec": "HttpApiSpec",
		"_foo":          "XFoo",
		"-foo":          "XFoo",
		"_Foo":          "XFoo",
		"-Foo":          "XFoo",
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			g := NewGomegaWithT(t)

			a := strcase.CamelCase(k)
			g.Expect(a).To(Equal(v))
		})
	}
}

func TestCamelCaseToKebabCase(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"Foo":                "foo",
		"FooBar":             "foo-bar",
		"foo9bar":            "foo9bar",
		"HTTPAPISpec":        "http-api-spec",
		"HTTPAPISpecBinding": "http-api-spec-binding",
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			g := NewGomegaWithT(t)

			a := strcase.CamelCaseToKebabCase(k)
			g.Expect(a).To(Equal(v))
		})
	}
}
