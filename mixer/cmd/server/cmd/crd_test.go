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

package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/mixer/pkg/adapter"
	pkgadapter "istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/template"
)

var empty = ``

var exampleAdapters = []pkgadapter.InfoFn{
	func() adapter.BuilderInfo { return adapter.BuilderInfo{Name: "foo-bar"} },
	func() adapter.BuilderInfo { return adapter.BuilderInfo{Name: "abcd"} },
}
var exampleAdaptersCrd = `
kind: CustomResourceDefinition
apiVersion: apiextensions.k8s.io/v1beta1
metadata:
  name: foo-bars.config.istio.io
  labels:
    package: foo-bar
    istio: mixer-adapter
spec:
  group: config.istio.io
  names:
    kind: foo-bar
    plural: foo-bars
    singular: foo-bar
  scope: Namespaced
  version: v1alpha2
---
kind: CustomResourceDefinition
apiVersion: apiextensions.k8s.io/v1beta1
metadata:
  name: abcds.config.istio.io
  labels:
    package: abcd
    istio: mixer-adapter
spec:
  group: config.istio.io
  names:
    kind: abcd
    plural: abcds
    singular: abcd
  scope: Namespaced
  version: v1alpha2
---`

var exampleTmplInfos = map[string]template.Info{
	"abcd-foo": {Name: "abcd-foo", Impl: "implPathShouldBeDNSCompat"},
	"abcdBar":  {Name: "abcdBar", Impl: "implPathShouldBeDNSCompat2"},
}
var exampleInstanceCrd = `kind: CustomResourceDefinition
apiVersion: apiextensions.k8s.io/v1beta1
metadata:
  name: abcd-foos.config.istio.io
  labels:
    package: implPathShouldBeDNSCompat
    istio: mixer-instance
spec:
  group: config.istio.io
  names:
    kind: abcd-foo
    plural: abcd-foos
    singular: abcd-foo
  scope: Namespaced
  version: v1alpha2
---
kind: CustomResourceDefinition
apiVersion: apiextensions.k8s.io/v1beta1
metadata:
  name: abcdBars.config.istio.io
  labels:
    package: implPathShouldBeDNSCompat2
    istio: mixer-instance
spec:
  group: config.istio.io
  names:
    kind: abcdBar
    plural: abcdBars
    singular: abcdBar
  scope: Namespaced
  version: v1alpha2
---
`

func TestListCrdsAdapters(t *testing.T) {
	tests := []struct {
		name    string
		args    []pkgadapter.InfoFn
		wantOut string
	}{
		{"empty", []pkgadapter.InfoFn{}, empty},
		{"example", exampleAdapters, exampleAdaptersCrd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buffer bytes.Buffer
			var printf = func(format string, args ...interface{}) {
				buffer.WriteString(fmt.Sprintf(format, args...))
			}
			listCrdsAdapters(printf, printf, tt.args)
			gotOut := buffer.String()

			if strings.TrimSpace(gotOut) != strings.TrimSpace(tt.wantOut) {
				t.Errorf("listCrdsAdapters() = %s, want %s", gotOut, tt.wantOut)
			}
		})
	}
}

func TestListCrdsInstances(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]template.Info
		wantOut string
	}{
		{"empty", map[string]template.Info{}, empty},
		{"example", exampleTmplInfos, exampleInstanceCrd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buffer bytes.Buffer
			var printf = func(format string, args ...interface{}) {
				buffer.WriteString(fmt.Sprintf(format, args...))
			}
			listCrdsInstances(printf, printf, tt.args)
			gotOut := buffer.String()

			if strings.TrimSpace(gotOut) != strings.TrimSpace(tt.wantOut) {
				t.Errorf("listCrdsInstances() = %v, want %v", gotOut, tt.wantOut)
			}
		})
	}
}
