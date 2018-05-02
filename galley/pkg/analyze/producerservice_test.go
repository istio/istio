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

package analyze

import (
	"strings"
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/istio/galley/pkg/api/service/dev"
)

func TestCheckServiceConfig(t *testing.T) {
	tests := []struct {
		config   string
		messages string
	}{
		{
			config: `
service:
  name: empty
`,
			messages: ``,
		},

		{
			config: `
service:
  name:
`,
			messages: `
[E0001] Field cannot be empty: service name
`,
		},

		{
			config: `
name: basic
instances:
  - name:
    template: report
    params:
      bar: baz
`,
			messages: `
[E0001] Field cannot be empty: instance name
`,
		},

		{
			config: `
name: basic
instances:
  - name: inst1
    template:
    params:
      bar: baz
`,
			messages: `
[E0001] Field cannot be empty: instance template
`,
		},
	}

	for _, tst := range tests {
		t.Run("", func(t *testing.T) {
			cfg := hydrateServiceConfig(t, tst.config)
			msgs := ProducerService(cfg)
			actual := msgs.String()
			actual = strings.TrimSpace(actual)
			expected := strings.TrimSpace(tst.messages)
			if actual != expected {
				t.Fatalf("Mismatch:\ngot:\n%v\nwanted:\n%v\nfor:\n%s", actual, expected, tst.config)
			}
		})
	}
}

func hydrateServiceConfig(t *testing.T, cfg string) *dev.ProducerService {
	r := dev.ProducerService{}
	err := yaml.Unmarshal([]byte(cfg), &r)
	if err != nil {
		t.Fatalf("error hydrating service config: %v", err)
	}
	return &r
}
