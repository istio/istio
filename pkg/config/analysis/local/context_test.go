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
package local

import (
	"testing"
	"time"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestIstiodContextTimeout(t *testing.T) {
	shortTime := time.Microsecond
	longTime := time.Second

	tests := []struct {
		name     string
		timeout  time.Duration
		waitTime time.Duration
		canceled bool
	}{
		{
			name:     "exceed timeout",
			timeout:  shortTime,
			waitTime: longTime,
			canceled: true,
		},
		{
			name:     "not exceed timeout",
			timeout:  longTime,
			waitTime: shortTime,
			canceled: false,
		},
	}

	for _, test := range tests {
		ctx := NewIstiodContextWithTimeout(nil, make(chan struct{}), func(name collection.Name) {}, test.timeout)
		testTimeout(ctx, t, test.name, test.waitTime, test.canceled)
	}
}

func testTimeout(ctx analysis.Context, t *testing.T, name string, wait time.Duration, canceled bool) {
	ch := time.After(wait)
	<-ch
	if ctx.Canceled() != canceled {
		t.Fatalf("test %s failed: expected %t got %t", name, canceled, ctx.Canceled())
	}
}
