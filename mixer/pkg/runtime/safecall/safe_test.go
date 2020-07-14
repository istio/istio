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

package safecall

import (
	"testing"
)

func TestSafeCall(t *testing.T) {
	worked := false
	err := Execute("m", func() {
		worked = true
	})

	if !worked {
		t.Fail()
	}

	if err != nil {
		t.Fail()
	}
}

func TestSafeCall_Panic(t *testing.T) {
	err := Execute("m", func() {
		panic("panic")
	})

	if err == nil {
		t.Fail()
	}
}

func TestSafeCall_Panic_Nil(t *testing.T) {
	err := Execute("m", func() {
		panic(nil)
	})

	if err == nil {
		t.Fail()
	}
}
