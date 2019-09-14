// Copyright Istio Authors. All Rights Reserved.
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

package testdata

import (
	"testing"
	"time"
)

// nolint: testlinter
func TestInvalidSkip(t *testing.T) {
	t.Skip("invalid skip without url to GitHub issue.")
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	t.Skip("https://github.com/istio/istio/issues/6041")
}

// nolint: testlinter
func TestInvalidShort(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}

	if testing.Short() {
		t.Skip("skipping uint test in short mode.")
	}

	if Count(3) != 3 {
		t.Error("expected 3")
	}
}

// nolint: testlinter
func TestInvalidSleep(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}
	time.Sleep(100 * time.Millisecond)

	if Count(3) != 3 {
		t.Error("expected 3")
	}
}

// nolint: testlinter
func TestInvalidGoroutine(t *testing.T) {
	go SetCount()
	if Count(5) != 5 {
		t.Error("expected 5")
	}
	if Count(7) != 7 {
		t.Error("expected 7")
	}
	if Count(9) != 9 {
		t.Error("expected 9")
	}
}
