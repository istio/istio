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

package restart_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/envoy/restart"
	"istio.io/istio/pkg/envoy/test"
)

func TestRestartEnvoy(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)

	h := test.NewManager(t)
	defer h.Cleanup(t)

	// Start Envoy.
	event1 := h.Run().ExpectEventSequence(t, restart.Starting, restart.EnvoyStarting)
	g.Expect(event1.Envoy.Epoch()).To(Equal(envoy.Epoch(0)))

	// Start another Envoy. If we were creating "real" Envoys, doing this would trigger the hot restart logic,
	// which would cause the first Envoy instance to drain and then terminate.
	h.Restart()
	event2 := h.ExpectOneEvent(t, restart.EnvoyStarting)
	g.Expect(event2.Envoy.Epoch()).To(Equal(envoy.Epoch(1)))

	h.Shutdown(t)
}

func TestAbortShouldRestartEnvoy(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)

	h := test.NewManager(t)
	defer h.Cleanup(t)

	// Run the manager and start Envoy.
	h.Run().WaitForEnvoy(t)
	h.ExpectEventSequence(t, restart.Starting, restart.EnvoyStarting)
	h.ExpectNoEvent(t)

	// Abort Envoy.
	h.AbortEnvoy(envoy.Epoch(0))
	event := h.ExpectNextEvent(t, restart.EnvoyStopped)
	g.Expect(event.Envoy.Epoch()).To(Equal(event.Envoy.Epoch()))
	g.Expect(event.Error).To(Equal(context.Canceled))

	// Verify envoy was restarted.
	h.WaitAndVerifyEnvoy(t, 0, false)
	event = h.ExpectOneEvent(t, restart.EnvoyStarting)
	g.Expect(event.Envoy.Epoch()).To(Equal(envoy.Epoch(0)))

	h.Shutdown(t)
}

func TestEnvoyCreationErrorShouldStopManager(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)

	h := test.NewManager(t)
	defer h.Cleanup(t)

	// Configure the envoy factory to return an error.
	err := errors.New("fake creation error")
	h.EnvoyFactory.SetCreationError(err)

	// Start Envoy for the first time and verify that no Envoy is successfully created.
	stoppedEvent := h.Run().ExpectEventSequence(t, restart.Starting, restart.Stopped)
	g.Expect(stoppedEvent.Error).To(Equal(err))
}

func TestEnvoyExitWithErrorShouldRestartEnvoy(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)

	h := test.NewManager(t)
	defer h.Cleanup(t)

	// Run the manager.
	e := h.Run().WaitForEnvoy(t)
	h.ExpectEventSequence(t, restart.Starting, restart.EnvoyStarting)
	h.ExpectNoEvent(t)

	// Exit Envoy with an error.
	exitErr := errors.New("fake error")
	e.Exit(exitErr)
	event := h.ExpectNextEvent(t, restart.EnvoyStopped)
	g.Expect(event.Envoy.Epoch()).To(Equal(e.Epoch()))
	g.Expect(event.Error).ToNot(BeNil())
	g.Expect(event.Error.Error()).To(ContainSubstring(exitErr.Error()))

	// Verify envoy was restarted.
	h.WaitAndVerifyEnvoy(t, 0, false)
	event = h.ExpectOneEvent(t, restart.EnvoyStarting)
	g.Expect(event.Envoy.Epoch()).To(Equal(e.Epoch()))

	h.Shutdown(t)
}

func TestRetryBudgetExceededShouldReturnError(t *testing.T) {
	t.Parallel()

	h := test.NewManager(t)
	defer h.Cleanup(t)

	// Only allow a single envoy to be created.
	h.Config.MaxRestartAttempts = 1
	h.Config.InitialRestartInterval = 10 * time.Millisecond

	// Run the manager and wait for Envoy1 to be created.
	e1 := h.Run().WaitForEnvoy(t)
	h.ExpectEventSequence(t, restart.Starting, restart.EnvoyStarting)
	h.ExpectNoEvent(t)

	// Exit Envoy1
	e1.Exit(errors.New("fake error 1"))

	// Wait for the Envoy2 to be created.
	e2 := h.WaitForEnvoy(t)
	h.ExpectEventSequence(t, restart.EnvoyStopped, restart.EnvoyStarting)
	h.ExpectNoEvent(t)

	// Exit Envoy2
	e2.Exit(errors.New("fake error 2"))

	// Wait for the manager to exit.
	event := h.WaitForEvent(t, restart.Stopped)
	if event.Error == nil {
		t.Fatalf("expected error")
	} else if !strings.Contains(event.Error.Error(), "budget exhausted") {
		t.Fatalf("expected budget exhausted, but got %v", event.Error)
	}
}

func TestExceedingRateLimitShouldDelayEnvoyStart(t *testing.T) {
	t.Parallel()

	h := test.NewManager(t).Run()
	defer h.Cleanup(t)

	startTime := time.Now()

	// Blast the manager with restarts. The rate limiter allows a burst of up to 10, so
	// we send a total of 11 (10 + the initial start) to force a wait.
	numRestarts := 11
	actualSecondsSinceStart := make([]int, 0)
	for epoch := 0; epoch < numRestarts; epoch++ {
		if epoch > 0 {
			h.Restart()
		}
		h.WaitAndVerifyEnvoy(t, envoy.Epoch(epoch), false)

		timeSinceStart := time.Since(startTime)
		actualSecondsSinceStart = append(actualSecondsSinceStart, int(timeSinceStart.Seconds()))
	}

	// Due to the rate limiter's burst=10, expect only the last state change to be delayed until the next second.
	expectedSecondsSinceStart := []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	if !reflect.DeepEqual(expectedSecondsSinceStart, actualSecondsSinceStart) {
		t.Fatalf("expected secondsSinceStart:\n%v+\nto equal:\n%v+", actualSecondsSinceStart, expectedSecondsSinceStart)
	}

	h.Shutdown(t)
}

// TestCascadingAbort plays a scenario that may trigger self-abort deadlock:
//  * start three epochs 0, 1, 2
//  * epoch 2 crashes, triggers abort of 0, 1
//  * epoch 1 crashes before receiving abort, triggers abort of 0
//  * epoch 0 aborts, recovery to config 2 follows
func TestCascadingAbort(t *testing.T) {
	t.Parallel()

	h := test.NewManager(t)
	defer h.Cleanup(t)

	// Set the initial retry to something large, so we won't retry crashing epochs during the test.
	h.Config.InitialRestartInterval = 1 * time.Minute

	crash1 := make(chan struct{})
	crash2 := make(chan struct{})

	// Run Envoy epoch 0, which will complete the abort after epoch 1 and 2 have crashed.
	envoy0 := h.Run().WaitAndVerifyEnvoy(t, envoy.Epoch(0), false)
	envoy0.SetCustomDoneHandler(func() {
		// Wait for epoch 1 and 2 to crash.
		<-crash2
		<-crash1

		// Add a short delay before aborting
		<-time.After(100 * time.Millisecond)
	})

	// Create epoch 1, which will crash after epoch 2 crashes.
	envoy1 := h.Restart().WaitAndVerifyEnvoy(t, envoy.Epoch(1), false)
	envoy1.SetCustomDoneHandler(func() {
		// Wait for epoch 2 to crash.
		<-crash2

		// Ignore abort and crash by itself
		<-time.After(100 * time.Millisecond)
		close(crash1)
		envoy1.SetWaitErr(errors.New("planned crash for epoch 1"))
	})

	// Create epoch 2.
	envoy2 := h.Restart().WaitAndVerifyEnvoy(t, envoy.Epoch(2), false)

	// Crash epoch 2, creating a cascading abort.
	envoy2.Exit(errors.New("planned crash for epoch 2"))
	close(crash2)

	<-crash2
	<-crash1
	<-time.After(500 * time.Millisecond)

	h.Shutdown(t)
}
