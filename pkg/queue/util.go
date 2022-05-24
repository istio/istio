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

package queue

import (
	"fmt"
	"time"
)

// WaitForClose blocks until the Instance has stopped processing tasks or the timeout expires.
// If the timeout is zero, it will wait until the queue is done processing.
// WaitForClose an error if the timeout expires.
func WaitForClose(q Instance, timeout time.Duration) error {
	closed := q.Closed()
	if timeout == 0 {
		<-closed
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
		return nil
	case <-timer.C:
		return fmt.Errorf("timeout waiting for queue to close after %v", timeout)
	}
}
