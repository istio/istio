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

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestIstiodContextTimeout(t *testing.T) {

	shortTime := time.Microsecond
	longTime := time.Second

	tests := []struct {
		timeout  time.Duration
		waitTime time.Duration
		expect   bool
	}{
		// exceed timeout
		{
			timeout:  shortTime,
			waitTime: longTime,
			expect:   true,
		},
		// not exceed timeout
		{
			timeout:  longTime,
			waitTime: shortTime,
			expect:   false,
		},
		// timeout immediately
		{
			timeout:  0,
			waitTime: 0,
			expect:   true,
		},
	}

	for _, test := range tests {
		ctx := NewIstiodContextWithTimeout(nil, make(chan struct{}), func(name collection.Name) {}, test.timeout)
		testTimeout(ctx, test.waitTime, t, test.expect)
	}
}

func testTimeout(ctx analysis.Context, wait time.Duration, t *testing.T, expected bool) {
	g := NewWithT(t)
	select {
	case <-time.After(wait):
		g.Expect(ctx.Canceled()).To(Equal(expected))
	}
}
