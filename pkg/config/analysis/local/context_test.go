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

	"istio.io/istio/pkg/config/schema/collection"
)

func TestIstiodContextTimeout(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		timeout   time.Duration
		sleepTime time.Duration
		expect    bool
	}{
		{
			timeout:   time.Microsecond,
			sleepTime: time.Millisecond,
			expect:    true,
		},
		{
			timeout:   time.Millisecond,
			sleepTime: time.Microsecond,
			expect:    false,
		},
	}

	for _, test := range tests {
		ctx := NewIstiodContextWithTimeout(nil, make(chan struct{}), func(name collection.Name) {}, test.timeout)
		time.Sleep(test.sleepTime)
		g.Expect(ctx.Canceled()).To(Equal(test.expect))
	}
}
