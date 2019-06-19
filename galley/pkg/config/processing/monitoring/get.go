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

package monitoring

import "sync"

var (
	mu       sync.Mutex
	instance Reporter
)

var getPatchTable = struct{ createOpenCensusReporter func() (*reporter, error) }{
	createOpenCensusReporter: createOpenCensusReporter,
}

// Get returns a singleton instance of a reporter.
func Get() (Reporter, error) {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		var err error
		if instance, err = getPatchTable.createOpenCensusReporter(); err != nil {
			return nil, err
		}
	}
	return instance, nil
}
