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
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/fuzz"
	"istio.io/istio/pkg/test/util/assert"
)

func FuzzShallowcopyTrafficPolicy(f *testing.F) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		r := fuzz.Struct[*networking.TrafficPolicy](fg)
		copied := ShallowcopyTrafficPolicy(r)
		assert.Equal(fg.T(), r, copied)
	})
}
