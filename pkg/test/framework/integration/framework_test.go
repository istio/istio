// Copyright 2019 Istio Authors
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

package integration

import (
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
)

func TestParallel(t *testing.T) {
	var top, l1a, l1b, l2a, l2b, l2c, l2d *component

	closeTimes := make(map[string]time.Time)
	mutex := &sync.Mutex{}
	closeHandler := func(c *component) {
		mutex.Lock()
		defer mutex.Unlock()
		closeTimes[c.name] = time.Now()

		// Sleep briefly to force time separation between close events.
		time.Sleep(100 * time.Millisecond)
	}

	assertClosedBefore := func(t *testing.T, c1, c2 *component) {
		t.Helper()
		if !closeTimes[c1.name].Before(closeTimes[c2.name]) {
			t.Fatalf("%s closed after %s", c1.name, c2.name)
		}
	}

	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("top").
				Run(func(ctx framework.TestContext) {
					// NOTE: top can't be parallel for this test since it will exit before the children run,
					// which means we won't be able to verify the results here.
					top = newComponent(ctx, ctx.Name(), closeHandler)

					ctx.NewSubTest("l1a").
						RunParallel(func(ctx framework.TestContext) {
							l1a = newComponent(ctx, ctx.Name(), closeHandler)

							ctx.NewSubTest("l2a").
								RunParallel(func(ctx framework.TestContext) {
									l2a = newComponent(ctx, ctx.Name(), closeHandler)
								})

							ctx.NewSubTest("l2b").
								RunParallel(func(ctx framework.TestContext) {
									l2b = newComponent(ctx, ctx.Name(), closeHandler)
								})
						})

					ctx.NewSubTest("l1b").
						RunParallel(func(ctx framework.TestContext) {
							l1b = newComponent(ctx, ctx.Name(), closeHandler)

							ctx.NewSubTest("l2c").
								RunParallel(func(ctx framework.TestContext) {
									l2c = newComponent(ctx, ctx.Name(), closeHandler)
								})

							ctx.NewSubTest("l2d").
								RunParallel(func(ctx framework.TestContext) {
									l2d = newComponent(ctx, ctx.Name(), closeHandler)
								})
						})
				})
		})

	assertClosedBefore(t, l2a, l1a)
	assertClosedBefore(t, l2b, l1a)
	assertClosedBefore(t, l2c, l1b)
	assertClosedBefore(t, l2d, l1b)
	assertClosedBefore(t, l1a, top)
	assertClosedBefore(t, l1b, top)
}
