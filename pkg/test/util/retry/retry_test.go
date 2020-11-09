//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package retry

import (
	"fmt"
	"testing"
	"time"
)

func TestConverge(t *testing.T) {
	t.Run("no converge", func(t *testing.T) {
		flipFlop := true
		err := UntilSuccess(func() error {
			flipFlop = !flipFlop
			if flipFlop {
				return fmt.Errorf("flipFlop was true")
			}
			return nil
		}, Converge(2), Timeout(time.Millisecond*10), Delay(time.Millisecond))
		if err == nil {
			t.Fatal("expected no convergence, but test passed")
		}
	})

	t.Run("converge", func(t *testing.T) {
		n := 0
		err := UntilSuccess(func() error {
			n++
			if n < 10 {
				return fmt.Errorf("%v is too low, try again", n)
			}
			return nil
		}, Converge(2), Timeout(time.Second*10000), Delay(time.Millisecond))
		if err != nil {
			t.Fatalf("expected convergance, but test failed: %v", err)
		}
	})
}
