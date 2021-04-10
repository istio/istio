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

package xds

import (
	"context"
	"time"
)

type (
	SendHandler  func(errorChan chan error)
	ErrorHandler func(err error)
)

// Send with timeout if specified. If timeout is zero, sends without timeout.
func Send(ctx context.Context, send SendHandler, errorh ErrorHandler, timeout time.Duration) error {
	errChan := make(chan error, 1)

	go func() {
		send(errChan)
		close(errChan)
	}()

	if timeout.Nanoseconds() > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		select {
		case <-timeoutCtx.Done():
			errorh(timeoutCtx.Err())
			return timeoutCtx.Err()
		case err := <-errChan:
			errorh(err)
			return err
		}
	} else {
		err := <-errChan
		errorh(err)
		return err
	}
}
