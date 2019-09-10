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

package restart

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestNewStrategy(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)

	s := newRetryStrategy(10, 10*time.Millisecond)
	g.Expect(s.Budget()).To(Equal(uint(10)))
}

func TestSchedule(t *testing.T) {
	t.Parallel()

	maxRetries := uint(1)
	interval := 10 * time.Millisecond
	s := newRetryStrategy(maxRetries, interval)

	expectedTime := time.Now().Add(interval)
	g := NewGomegaWithT(t)
	g.Expect(schedule(t, s)).To(BeNil())
	expectTimerAt(t, s, expectedTime)
}

func TestScheduleWithNoRetriesShouldFail(t *testing.T) {
	t.Parallel()

	s := newRetryStrategy(0, 10*time.Millisecond)

	g := NewGomegaWithT(t)
	g.Expect(schedule(t, s)).ToNot(BeNil())
}

func TestExhaustBudget(t *testing.T) {
	t.Parallel()

	s := newRetryStrategy(1, 10*time.Millisecond)

	g := NewGomegaWithT(t)

	// The first attempt should succeed.
	g.Expect(schedule(t, s)).To(BeNil())
	g.Expect(s.Budget()).To(Equal(uint(0)))

	// Now cancel the pending timer.
	s.Cancel()

	// The second attempt should fail.
	g.Expect(schedule(t, s)).ToNot(BeNil())
}

func TestExponentialBackOff(t *testing.T) {
	t.Parallel()

	maxRetries := uint(3)
	interval := 200 * time.Millisecond
	s := newRetryStrategy(maxRetries, interval)

	g := NewGomegaWithT(t)

	tExpected := time.Now().Add(interval)
	g.Expect(schedule(t, s)).To(BeNil())
	g.Expect(s.Budget()).To(Equal(maxRetries - 1))
	expectTimerAt(t, s, tExpected)

	tExpected = time.Now().Add(interval * 2)
	g.Expect(schedule(t, s)).To(BeNil())
	g.Expect(s.Budget()).To(Equal(maxRetries - 2))
	expectTimerAt(t, s, tExpected)

	tExpected = time.Now().Add(interval * 4)
	g.Expect(schedule(t, s)).To(BeNil())
	g.Expect(s.Budget()).To(Equal(maxRetries - 3))
	expectTimerAt(t, s, tExpected)

	// Last attempt should exhaust the budget.
	g.Expect(schedule(t, s)).ToNot(BeNil())
}

func TestScheduleTwiceWillDoNothing(t *testing.T) {
	t.Parallel()

	interval := 500 * time.Millisecond
	s := newRetryStrategy(1, interval)

	g := NewGomegaWithT(t)

	tExpected := time.Now().Add(interval)
	g.Expect(schedule(t, s)).To(BeNil())
	newScheduleCreated, err := s.Schedule()
	g.Expect(err).To(BeNil())
	g.Expect(newScheduleCreated).To(BeFalse())
	expectTimerAt(t, s, tExpected)
}

func TestReset(t *testing.T) {
	t.Parallel()

	s := newRetryStrategy(1, 0)

	g := NewGomegaWithT(t)

	f := func() {
		// The first attempt should succeed.
		g.Expect(schedule(t, s)).To(BeNil())

		// Now cancel the pending timer.
		s.Cancel()

		// The second attempt should fail.
		g.Expect(schedule(t, s)).ToNot(BeNil())
	}

	f()

	// Reset the budget.
	s.Reset()

	f()
}

func TestChanWithNoScheduleShouldNeverFire(t *testing.T) {
	t.Parallel()

	s := newRetryStrategy(1, 0)
	expectNoTimer(t, s)
}

func schedule(t *testing.T, s *retryStrategy) error {
	t.Helper()
	newScheduleCreated, err := s.Schedule()
	if err == nil && !newScheduleCreated {
		t.Fatal("new schedule not created")
	}
	return err
}

func expectTimerAt(t *testing.T, s *retryStrategy, expectedTime time.Time) {
	t.Helper()
	select {
	case <-s.Chan():
		g := NewGomegaWithT(t)
		g.Expect(time.Now()).To(BeTemporally("~", expectedTime, 100*time.Millisecond))
		s.Cancel()
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for retry timer")
	}
}

func expectNoTimer(t *testing.T, s *retryStrategy) {
	t.Helper()
	select {
	case <-s.Chan():
		t.Fatal("unexpected timer event")
	case <-time.After(1 * time.Second):
		// Successful.
		return
	}
}
