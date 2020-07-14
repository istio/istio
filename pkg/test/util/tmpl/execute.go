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
	"bytes"
	"text/template"

	"istio.io/istio/pkg/test"
)

// Execute the template with the given parameters.
func Execute(t *template.Template, data interface{}) (string, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}

	return b.String(), nil
}

// ExecuteOrFail calls Execute and fails the test if it returns an error.
func ExecuteOrFail(t test.Failer, t2 *template.Template, data interface{}) string {
	t.Helper()
	s, err := Execute(t2, data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
