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

package generator

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/istio/galley/pkg/api"
	"istio.io/istio/galley/pkg/api/distrib"
)

type xformTest struct {
	svc   string
	mixer string
	err   error
}

var tests = []xformTest{
	{
		svc: `
name: foo
rules:
  - match: foo == bar
    actions:
      - handler: stackdriver
        instances:
          - template: quota
            params:
              foo: bar
          - ref: logstuff

handlers:
  - name: stackdriver

instances:
  - name: logstuff
    template: report
    params:
      bar: baz
`,
		mixer: `
id: 00000000-0000-0000-0000-000000000000
instances:
  - name: logstuff
    params: {}
    template: report
  - name: _instance0
    params: {}
    template: quota
rules:
  - actions:
    - handler: stackdriver
      instances:
      - _instance0
      - logstuff
    match: foo == bar
`,
	},
}

func TestTransform(t *testing.T) {
	for i, tst := range tests {
		name := fmt.Sprintf("%d", i)

		t.Run(name, func(t *testing.T) {
			cfg := hydrateServiceConfig(t, tst.svc)
			out, err := Transform([]*api.ServiceConfig{cfg})
			if err != nil {
				if tst.err != err {
					t.Fatalf("Error mismatch:\ngot:%v\nwanted: %v\n", err, tst.err)
				}
			}

			out.Id = "00000000-0000-0000-0000-000000000000" // normalize the id of the config.
			expected := hydrateMixerConfig(t, tst.mixer)
			if !reflect.DeepEqual(out, expected) {
				t.Fatalf("Mismatch\n got:\n'%v'\nwanted:\n'%v'\n",
					serializeMixerConfig(t, out),
					serializeMixerConfig(t, expected))
			}
		})
	}
}

func hydrateServiceConfig(t *testing.T, cfg string) *api.ServiceConfig {
	r := api.ServiceConfig{}
	err := yaml.Unmarshal([]byte(cfg), &r)
	if err != nil {
		t.Fatalf("error hydrating service config: %v", err)
	}
	return &r
}

func hydrateMixerConfig(t *testing.T, cfg string) *distrib.MixerConfig {
	r := distrib.MixerConfig{}
	err := yaml.Unmarshal([]byte(cfg), &r)
	if err != nil {
		t.Fatalf("error hydrating mixer config: %v", err)
	}
	return &r
}

func serializeMixerConfig(t *testing.T, config *distrib.MixerConfig) string {
	bytes, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	return string(bytes)
}
