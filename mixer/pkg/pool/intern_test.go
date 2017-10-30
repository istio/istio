// Copyright 2016 Istio Authors
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

package pool

import (
	"testing"
)

func TestIntern(t *testing.T) {
	strings := []string{
		"a",
		"b",
		"c",
		"d",
		"e",
		"f",
		"g",
		"h",
	}

	// We're only testing the semantics here, we're not actually
	// verifying that interning has taken place. That's too hard to
	// do in Go...

	p := newStringPool(4)
	for _, s := range strings {
		r := p.Intern(s)
		if r != s {
			t.Errorf("Got mismatch %v != %v", r, s)
		}
	}

	for _, s := range strings {
		r := Intern(s)
		if r != s {
			t.Errorf("Got mismatch %v != %v", r, s)
		}
	}
}
