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

package util

import (
	"encoding/json"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/fuzz"
	"istio.io/istio/pkg/test/util/assert"
)

func FuzzShallowCopyTrafficPolicy(f *testing.F) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		r := fuzz.Struct[*networking.TrafficPolicy](fg)
		// We do not copy these, so ignore them
		r.PortLevelSettings = nil
		copied := ShallowCopyTrafficPolicy(r)
		assert.Equal(fg.T(), r, copied)
	})
}

func FuzzShallowCopyPortTrafficPolicy(f *testing.F) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		r := fuzz.Struct[*networking.TrafficPolicy_PortTrafficPolicy](fg)

		// The port does not need to be copied.
		r.Port = nil

		// The type has changed and cannot be compared directly.
		// Convert to []byte for comparison.
		copied := shadowCopyPortTrafficPolicy(r)
		rjs, _ := json.Marshal(r)
		cjs, _ := json.Marshal(copied)
		assert.Equal(fg.T(), rjs, cjs)
	})
}

func FuzzMergeTrafficPolicy(f *testing.F) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		copyFrom := fuzz.Struct[*networking.TrafficPolicy](fg)
		// Merge does not copy port level, so do not handle this
		checkFrom := copyFrom.DeepCopy()
		checkFrom.PortLevelSettings = nil

		empty1 := &networking.TrafficPolicy{}
		copied := mergeTrafficPolicy(empty1, copyFrom, true)
		assert.Equal(fg.T(), checkFrom, copied)

		empty2 := &networking.TrafficPolicy{}
		copied = mergeTrafficPolicy(empty2, copied, true)
		assert.Equal(fg.T(), checkFrom, copied)
	})
}
