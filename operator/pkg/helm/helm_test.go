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

package helm

import (
	"reflect"
	"testing"
)

func TestGetAddonNamesFromCharts(t *testing.T) {
	tc := []struct {
		name            string
		directory       string
		capitalize      bool
		expectedResult  []string
		expectedFailure bool
	}{
		{
			"valid_addon",
			"testdata/addons/a",
			false,
			[]string{"addon"},
			false,
		},
		{
			"valid_addon_capitalized",
			"testdata/addons/a",
			true,
			[]string{"Addon"},
			false,
		},
		{
			"duplicate_addon",
			"testdata/addons/invalid",
			true,
			nil,
			true,
		},
		{
			"vfs",
			"../../cmd/mesh/testdata/manifest-generate/data-snapshot",
			false,
			[]string{"grafana", "istiocoredns", "kiali", "prometheus", "prometheusOperator", "tracing"},
			false,
		},
	}
	for _, c := range tc {
		result, err := GetAddonNamesFromCharts(c.directory, c.capitalize)
		if err == nil && c.expectedFailure {
			t.Fatalf("%v: expected error but got nil", c.name)
		} else if err != nil && !c.expectedFailure {
			t.Fatalf("%v: expected no error but got %v", c.name, err)
		}
		if !reflect.DeepEqual(result, c.expectedResult) {
			t.Fatalf("%v: result did not match expectedResult (got: %v, wanted: %v)", c.name, result, c.expectedResult)
		}
	}
}
