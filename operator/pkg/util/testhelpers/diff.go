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

package testhelpers

import (
	"sigs.k8s.io/yaml"

	diff "istio.io/istio/pilot/test/util"
)

// YAMLDiff compares two YAML contents
func YAMLDiff(a, b string) string {
	// Round trip to normalize the format
	roundTrip := func(a string) []byte {
		out := map[string]any{}
		if err := yaml.Unmarshal([]byte(a), &out); err != nil {
			return []byte(a)
		}
		y, err := yaml.Marshal(out)
		if err != nil {
			return []byte(a)
		}
		return y
	}
	err := diff.Compare(roundTrip(a), roundTrip(b))
	if err != nil {
		return err.Error()
	}
	return ""
}
