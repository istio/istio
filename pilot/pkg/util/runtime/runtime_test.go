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

package runtime

import (
	"testing"
	"time"
)

func TestHandleCrash(t *testing.T) {
	defer func() {
		if x := recover(); x != nil {
			t.Errorf("Expected no panic ")
		}
	}()

	defer HandleCrash()
	panic("test")
}

func TestCustomHandleCrash(t *testing.T) {
	ch := make(chan struct{}, 1)
	defer func() {
		select {
		case <-ch:
			t.Logf("crash handler called")
		case <-time.After(1 * time.Second):
			t.Errorf("Custom handler not called")
		}
	}()

	defer HandleCrash(func(interface{}) {
		ch <- struct{}{}
	})

	panic("test")
}
