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

package safecall

import (
	"fmt"
)

// Execute run the fn and in case of any panic, will be return it as an error.
func Execute(name string, fn func()) (err error) {
	// Try to detect panic, even if panic was called with nil.
	reachedEnd := false
	defer func() {
		if reachedEnd {
			return
		}

		r := recover()
		err = fmt.Errorf("panic during %v: '%v' ", name, r)
	}()

	fn()

	reachedEnd = true
	return
}
