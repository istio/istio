// Copyright Istio Authors.
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

package yaml

import "strconv"

// ToFloat type casts input to float64 if it is possible.
func ToFloat(v interface{}) (float64, bool) {
	switch c := v.(type) {
	case float32:
		return float64(c), true
	case float64:
		return c, true
	default:
		// depending on the parser, `v` can be an integer if user does not explicitly append the value with `.0`.
		// For example user writes `3` instead of `3.0`. Below case takes care of treating integer
		// as float64.
		if d, ok := ToInt64(v); ok {
			return float64(d), true
		}
		return 0, false
	}
}

// ToInt64 type casts input to int64 if it is possible.
func ToInt64(v interface{}) (int64, bool) {
	switch c := v.(type) {
	case int:
		return int64(c), true
	case int8:
		return int64(c), true
	case int16:
		return int64(c), true
	case int32:
		return int64(c), true
	case int64:
		return c, true
	case float64:
		// json only supports 'number'
		// therefore even integers are encoded as float
		ii := int64(c)
		return ii, float64(ii) == c
	case string:
		ii, err := strconv.ParseInt(c, 0, 64)
		return ii, err == nil
	default:
		return 0, false
	}
}
