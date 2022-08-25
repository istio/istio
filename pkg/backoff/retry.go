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

package backoff

import "time"

// Retry the operation until it does not return error or BackOff stops.
// o is guaranteed to be run at least once.
// Retry sleeps the goroutine for the duration returned by BackOff after a
// failed operation returns.
func Retry(operation func() error, b BackOff) error {
	var err error
	var next time.Duration

	for {
		if err = operation(); err == nil {
			return nil
		}

		next = b.NextBackOff()
		if next == MaxDuration {
			return err
		}

		time.Sleep(next)
	}
}
