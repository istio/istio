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

package memquota

import (
	"testing"
)

func TestAlloc(t *testing.T) {
	cases := []struct {
		amount int64
		tick   int64
		avail  int64
		result bool
	}{
		{6, 1, 5, false},
		{1, 1, 4, true},
		{2, 1, 2, true},
		{2, 1, 0, true},
		{1, 1, 0, false},
		{2, 1, 0, false},
		{1, 2, 0, false},
		{1, 3, 0, false},
		{1, 4, 4, true},
		{4, 5, 0, true},
		{1, 5, 0, false},
		{1, 7, 0, true},
		{1, 7, 0, false},
		{1, 8, 3, true},
		{5, 11, 0, true},
	}

	w := newRollingWindow(5, 3)

	for i, c := range cases {
		if ok := w.alloc(c.amount, c.tick); ok != c.result {
			t.Errorf("Expecting %v got %v, case %d", c.result, ok, i)
		}

		if w.available() != c.avail {
			t.Errorf("Expecting %d available, got %d, case %d", c.avail, w.available(), i)
		}
	}
}

func TestRelease(t *testing.T) {
	cases := []struct {
		allocAmount   int64
		allocTick     int64
		releaseAmount int64
		releaseTick   int64
		releaseResult int64
		avail         int64
	}{
		{4, 0, 4, 0, 4, 5},
		{4, 0, 4, 1, 4, 5},
		{4, 0, 4, 4, 0, 5},

		{2, 10, 0, 10, 0, 3},
		{2, 11, 0, 11, 0, 1},
		{0, 13, 4, 13, 2, 5},
		{5, 13, 5, 13, 5, 5},
	}

	w := newRollingWindow(5, 3)

	for i, c := range cases {
		if ok := w.alloc(c.allocAmount, c.allocTick); !ok {
			t.Errorf("Expecting to succeed, case %d", i)
		}

		result := w.release(c.releaseAmount, c.releaseTick)
		if result != c.releaseResult {
			t.Errorf("Expecting %d, got %d, case %d", c.releaseResult, result, i)
		}

		if w.available() != c.avail {
			t.Errorf("Expecting %d available, got %d, case %d", c.avail, w.available(), i)
		}
	}
}
