//  Copyright 2018 Istio Authors
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

package common

import (
	"fmt"
	"sync"
)

// MockLog captures events happening in a mock
type MockLog struct {
	lock     sync.Mutex
	contents string
}

// Append a new, formatted line to the log
func (e *MockLog) Append(format string, args ...interface{}) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.contents += fmt.Sprintf(format, args...)
	e.contents += "\n"
}

// String returns the log contents
func (e *MockLog) String() string {
	e.lock.Lock()
	defer e.lock.Unlock()
	return e.contents
}
