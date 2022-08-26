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

import (
	"context"
	"fmt"
	"time"
)

// RetryWithContext tries the operation until it does not return error, BackOff stops,
// or when the context expires, whichever happens first.
// o is guaranteed to be run at least once.
// RetryWithContext sleeps the goroutine for the duration returned by BackOff after a
// failed operation returns.
func RetryWithContext(ctx context.Context, operation func() error, b BackOff) error {
	for {
		err := operation()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%v with last error: %v", context.DeadlineExceeded, err)
		default:
			next := b.NextBackOff()
			if next == MaxDuration {
				return err
			}
			time.Sleep(next)
		}
	}
}
