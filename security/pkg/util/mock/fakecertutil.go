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

package mock

import "time"

// FakeCertUtil is a mocked CertUtil for testing.
type FakeCertUtil struct {
	Duration time.Duration
	Err      error
}

// GetWaitTime returns duration if err is nil, otherwise, it returns err.
func (f FakeCertUtil) GetWaitTime(certBytes []byte, now time.Time, minGracePeriod time.Duration) (time.Duration, error) {
	if f.Err != nil {
		return time.Duration(0), f.Err
	}
	return f.Duration, nil
}
