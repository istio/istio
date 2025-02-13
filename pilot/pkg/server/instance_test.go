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

package server_test

import (
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pkg/test"
)

func TestStartWithError(t *testing.T) {
	g := NewWithT(t)

	inst := server.New()
	expected := errors.New("fake")
	inst.RunComponent("fake", func(stop <-chan struct{}) error {
		return expected
	})

	stop := newReclosableChannel()
	t.Cleanup(stop.Close)
	g.Expect(inst.Start(stop.c)).To(Equal(expected))
}

func TestStartWithNoError(t *testing.T) {
	g := NewWithT(t)

	inst := server.New()
	c := newFakeComponent(0, test.NewStop(t))
	inst.RunComponent("fake", c.Run)

	stop := newReclosableChannel()
	t.Cleanup(stop.Close)
	g.Expect(inst.Start(stop.c)).To(BeNil())
	g.Expect(c.started.Load()).To(BeTrue())
}

func TestRunComponentsAfterStart(t *testing.T) {
	longDuration := 10 * time.Second
	shortDuration := 10 * time.Millisecond
	// Just used to make sure we do not leak goroutines
	stop := test.NewStop(t)
	cases := []struct {
		name  string
		c     *fakeComponent
		async bool
		wait  bool
	}{
		{
			name: "RunComponent",
			// Use a large duration - it will not complete before the end of the test.
			// This is used to verify that we don't wait for it while shutting down.
			c:     newFakeComponent(longDuration, stop),
			async: false,
			wait:  false,
		},
		{
			name: "RunComponentAsync",
			// Use a large duration - it will not complete before the end of the test.
			// This is used to verify that we don't wait for it while shutting down.
			c:     newFakeComponent(longDuration, stop),
			async: true,
			wait:  false,
		},
		{
			name:  "RunComponentAsyncAndWait",
			c:     newFakeComponent(shortDuration, stop),
			async: true,
			wait:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)

			stop := newReclosableChannel()
			t.Cleanup(stop.Close)
			inst := server.New()
			g.Expect(inst.Start(stop.c)).To(BeNil())

			component := c.c.Run
			go func() {
				if c.async {
					if c.wait {
						inst.RunComponentAsyncAndWait("test", component)
					} else {
						inst.RunComponentAsync("test", component)
					}
				} else {
					inst.RunComponent("test", component)
				}
			}()

			// Ensure that the component is started.
			g.Eventually(func() bool {
				return c.c.started.Load()
			}).Should(BeTrue())

			// Stop before the tasks end.
			stop.Close()
			if c.wait {
				// Add a little buffer to the task duration.
				totalWaitTime := shortDuration + (1 * time.Second)
				g.Eventually(func() bool {
					return c.c.completed.Load()
				}, totalWaitTime).Should(BeTrue())
			} else {
				g.Expect(c.c.completed.Load()).Should(BeFalse())
			}
		})
	}
}

type reclosableChannel struct {
	c      chan struct{}
	closed bool
}

func newReclosableChannel() *reclosableChannel {
	return &reclosableChannel{
		c: make(chan struct{}),
	}
}

func (c *reclosableChannel) Close() {
	if !c.closed {
		c.closed = true
		close(c.c)
	}
}

type fakeComponent struct {
	started   *atomic.Bool
	completed *atomic.Bool
	d         time.Duration
	stop      chan struct{}
}

func newFakeComponent(d time.Duration, stop chan struct{}) *fakeComponent {
	return &fakeComponent{
		started:   atomic.NewBool(false),
		completed: atomic.NewBool(false),
		d:         d,
		stop:      stop,
	}
}

func (c *fakeComponent) Run(stop <-chan struct{}) error {
	c.started.Store(true)
	select {
	case <-time.After(c.d):
	case <-c.stop: // ignore incoming stop; for test purposes we use our own stop
	}
	c.completed.Store(true)
	return nil
}
