// Copyright 2019 Istio Authors
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

package timedfn

import (
	"fmt"
	"time"
)

// WithTimeout waits for the function to complete or the given timeout to expire.
func WithTimeout(fn func(), timeout time.Duration) error {
	c := make(chan struct{}, 1)
	go func() {
		defer close(c)
		fn()
	}()

	select {
	case <-c:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out while waiting for requested action to complete")
	}
}
