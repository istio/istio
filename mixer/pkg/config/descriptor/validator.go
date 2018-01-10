// Copyright 2017 Istio Authors.
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

package descriptor

import (
	"fmt"
	"math"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/adapter"
)

func validateDescriptorName(name string) (ce *adapter.ConfigErrors) {
	if name == "" {
		ce = ce.Appendf("name", "a descriptor's name must not be empty")
	}
	return
}

func validateLabels(labels map[string]dpb.ValueType) (ce *adapter.ConfigErrors) {
	for name, vt := range labels {
		if vt == dpb.VALUE_TYPE_UNSPECIFIED {
			ce = ce.Appendf(fmt.Sprintf("labels[%s]", name), "must have a value type, we got VALUE_TYPE_UNSPECIFIED")
		}
	}
	return
}

// Returns true iff there are less than or equal to 'steps' representable floating point values between a and b. Steps == 0 means
// two exactly equal floats; in practice allowing a few steps between floats is fine for equality. Typically if A is one
// step larger than B, A <= 1.000000119 * B (barring a few classes of exceptions).
//
// See https://randomascii.wordpress.com/2012/02/25/comparing-floating-point-numbers-2012-edition/ for more details.
func almostEq(a, b float64, steps int64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	} else if math.Signbit(a) != math.Signbit(b) {
		// They're different, but +0 == -0, so we check to be sure.
		return a == b
	}
	// When we interpret a floating point number's bits as an integer, the difference between two floats-as-ints
	// is one plus the number of representable floating point values between the two. This is called "Units in the
	// Last Place", or "ULPs".
	ulps := int64(math.Float64bits(a)) - int64(math.Float64bits(b))
	// Go's math package only supports abs on float64, we don't want to round-trip through float64 again, so we do it ourselves.
	if ulps < 0 {
		ulps = -ulps
	}
	return ulps <= steps
}
