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

package tmpl

import (
	"fmt"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"istio.io/istio/pkg/test"
)

// Parse the given template content.
func Parse(tpl string) (*template.Template, error) {
	t := template.New("test template")
	return t.Funcs(sprig.TxtFuncMap()).Parse(tpl)
}

// ParseOrFail calls Parse and fails tests if it returns error.
func ParseOrFail(t test.Failer, tpl string) *template.Template {
	t.Helper()
	tpl2, err := Parse(tpl)
	if err != nil {
		t.Fatalf("tmpl.ParseOrFail: %v", err)
	}
	return tpl2
}

// MustParse calls Parse and panics if it returns error.
func MustParse(tpl string) *template.Template {
	tpl2, err := Parse(tpl)
	if err != nil {
		panic(fmt.Sprintf("tmpl.MustParse: %v", err))
	}
	return tpl2
}
