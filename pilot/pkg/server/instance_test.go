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
)

func TestStartWithError(t *testing.T) {
	g := NewWithT(t)

	inst := server.New()
	expected := errors.New("fake")
	inst.RunComponent(func(stop <-chan struct{}) error {
		return expected
	})

	stop := newReclosableChannel()
	t.Cleanup(stop.Close)
	g.Expect(inst.Start(stop.c)).To(Equal(expected))
}

func TestStartWithNoError(t *testing.T) {
	g := NewWithT(t)

	inst := server.New()
	c := newFakeComponent(0)
	inst.RunComponent(c.Run)

	stop := newReclosableChannel()
	t.Cleanup(stop.Close)
	g.Expect(inst.Start(stop.c)).To(BeNil())
	g.Expect(c.started.Load()).To(BeTrue())
}

func TestRunComponentsAfterStart(t *testing.T) {
	longDuration := 10 * time.Second
	shortDuration := 1 * time.Second
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
			c:     newFakeComponent(longDuration),
			async: false,
			wait:  false,
		},
		{
			name: "RunComponentAsync",
			// Use a large duration - it will not complete before the end of the test.
			// This is used to verify that we don't wait for it while shutting down.
			c:     newFakeComponent(longDuration),
			async: true,
			wait:  false,
		},
		{
			name:  "RunComponentAsyncAndWait",
			c:     newFakeComponent(shortDuration),
			async: true,
			wait:  true,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)

			stop := newReclosableChannel()
			t.Cleanup(stop.Close)
			inst := server.New()
			g.Expect(inst.Start(stop.c)).To(BeNil())

			go func() {
				component := c.c.Run
				if c.async {
					if c.wait {
						inst.RunComponentAsyncAndWait(component)
					} else {
						inst.RunComponentAsync(component)
					}
				} else {
					inst.RunComponent(component)
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
}

func newFakeComponent(d time.Duration) *fakeComponent {
	return &fakeComponent{
		started:   atomic.NewBool(false),
		completed: atomic.NewBool(false),
		d:         d,
	}
}

func (c *fakeComponent) Run(_ <-chan struct{}) error {
	c.started.Store(true)
	time.Sleep(c.d)
	c.completed.Store(true)
	return nil
}
