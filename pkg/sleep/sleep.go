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

package sleep

import (
	"context"
	"time"
)

// UntilContext sleeps for the given duration, or until the context is complete.
// Returns true if the sleep completes the full duration
func UntilContext(ctx context.Context, d time.Duration) bool {
	return Until(ctx.Done(), d)
}

// Until sleeps for the given duration, or until the channel is closed.
// Returns true if the sleep completes the full duration
func Until(ch <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	select {
	case <-ch:
		if !timer.Stop() {
			<-timer.C
		}
		return false
	case <-timer.C:
		return true
	}
}
