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

package bootstrap

import (
	"reflect"
	"testing"

	"k8s.io/kubectl/pkg/util/fieldpath"
)

func TestParseDownwardApi(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]string
	}{
		{
			"empty",
			map[string]string{},
		},
		{
			"single",
			map[string]string{"foo": "bar"},
		},
		{
			"multi",
			map[string]string{
				"app":               "istio-ingressgateway",
				"chart":             "gateways",
				"heritage":          "Tiller",
				"istio":             "ingressgateway",
				"pod-template-hash": "54756dbcf9",
			},
		},
		{
			"multi line",
			map[string]string{
				"config": `foo: bar
other: setting`,
				"istio": "ingressgateway",
			},
		},
		{
			"weird values",
			map[string]string{
				"foo": `a1_-.as1`,
				"bar": `a=b`,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Using the function kubernetes actually uses to write this, we do a round trip of
			// map -> file -> map and ensure the input and output are the same
			got, err := ParseDownwardAPI(fieldpath.FormatMap(tt.m))
			if !reflect.DeepEqual(got, tt.m) {
				t.Fatalf("expected %v, got %v with err: %v", tt.m, got, err)
			}
		})
	}
}
