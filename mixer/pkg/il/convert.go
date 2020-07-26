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

package il

import "math"

// IntegerToByteCode converts a Go int64 to its byte-code form.
func IntegerToByteCode(i int64) (uint32, uint32) {
	return uint32(i & 0xFFFFFFFF), uint32(i >> 32)
}

// ByteCodeToInteger extracts a Go int64 from its byte-code form.
func ByteCodeToInteger(o1, o2 uint32) int64 {
	return int64(o1) + (int64(o2) << 32)
}

// DoubleToByteCode converts a Go float64 to its byte-code form.
func DoubleToByteCode(d float64) (uint32, uint32) {
	u64 := math.Float64bits(d)
	return uint32(u64 & 0xFFFFFFFF), uint32(u64 >> 32)
}

// ByteCodeToDouble extracts a Go float64 from its byte-code form.
func ByteCodeToDouble(o1, o2 uint32) float64 {
	return math.Float64frombits(uint64(o1) + (uint64(o2) << 32))
}

// BoolToByteCode converts a Go bool to its byte-code form.
func BoolToByteCode(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// ByteCodeToBool extracts a Go bool from its byte-code form.
func ByteCodeToBool(o uint32) bool {
	return o != 0
}
