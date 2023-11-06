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

package param_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/util/tmpl"
)

func TestContains(t *testing.T) {
	cases := []struct {
		name        string
		template    string
		expectFound bool
	}{
		{
			name:        "empty",
			template:    "",
			expectFound: false,
		},
		{
			name:        "wrong param",
			template:    "{{ .Other.some.thing }}",
			expectFound: false,
		},
		{
			name:        "basic",
			template:    "{{ .To }}",
			expectFound: true,
		},
		{
			name:        "with method calls",
			template:    "{{ .To.some.thing }}",
			expectFound: true,
		},
		{
			name:        "if",
			template:    "{{ if .To.some.thing }}{{ end }}",
			expectFound: true,
		},
		{
			name:        "range",
			template:    "{{- range $key, $val := .To.some.thing }}{{ $key }}{{ $val }}{{- end }}",
			expectFound: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl := param.Parse(tmpl.ParseOrFail(t, c.template))
			actual := tpl.ContainsWellKnown(param.To)

			g := NewWithT(t)
			g.Expect(actual).To(Equal(c.expectFound))
		})
	}
}
