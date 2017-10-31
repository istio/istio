// Copyright 2017 Istio Authors
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

package ilt

import (
	"bytes"
)

// AreEqual checks for equality of given values. It handles comparison of []byte as a special case.
func AreEqual(e interface{}, a interface{}) bool {
	if eb, ok := e.([]byte); ok {
		if ab, ok := a.([]byte); ok {
			return bytes.Equal(ab, eb)
		} else {
			return false
		}
	}

	return a == e
}
