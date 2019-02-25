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

package util_test

import (
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/util"
)

func TestStopBeforeStartShouldNotHaveEffect(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	preStopCalled := false
	postStopCalled := false
	w.Stop(func() { preStopCalled = true }, func() { postStopCalled = true })
	g.Expect(preStopCalled).To(BeFalse())
	g.Expect(postStopCalled).To(BeFalse())
}

func TestStartTwiceShouldCallFuncOnce(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	count := 0
	startFn := func(stopCh chan struct{}, stoppedCh chan struct{}) error {
		count++
		return nil
	}

	err := w.Start(startFn)
	g.Expect(err).To(BeNil())
	g.Expect(count).To(Equal(1))

	// Second call should fail and not call startFn.
	err = w.Start(startFn)
	g.Expect(err).ToNot(BeNil())
	g.Expect(count).To(Equal(1))
}

func TestStartWithError(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	expected := errors.New("fake error")
	actual := w.Start(func(stopCh chan struct{}, stoppedCh chan struct{}) error {
		return expected
	})
	g.Expect(actual).To(Equal(expected))
}

func TestStopTwiceShouldCallFuncOnce(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	// Start the worker thread.
	err := w.Start(func(stopCh chan struct{}, stoppedCh chan struct{}) error {
		go func() {
			// Wait for the stop signal.
			<-stopCh
			// Signal that the thread has terminated.
			close(stoppedCh)
		}()
		return nil
	})
	g.Expect(err).To(BeNil())

	// Stop the worker.
	preStopCount := 0
	preStopFn := func() {
		preStopCount++
	}
	postStopCount := 0
	postStopFn := func() {
		postStopCount++
	}
	w.Stop(preStopFn, postStopFn)
	g.Expect(preStopCount).To(Equal(1))
	g.Expect(postStopCount).To(Equal(1))

	// Second call should do nothing.
	w.Stop(preStopFn, postStopFn)
	g.Expect(preStopCount).To(Equal(1))
	g.Expect(postStopCount).To(Equal(1))
}

func TestPreStopBeforePostStop(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	// Start the worker thread.
	var stopTime time.Time
	err := w.Start(func(stopCh chan struct{}, stoppedCh chan struct{}) error {
		go func() {
			// Wait for the stop signal.
			<-stopCh

			// Store the time that the stop was received.
			stopTime = time.Now()

			// Sleep for a bit to force the post time to be later.
			time.Sleep(10 * time.Millisecond)

			// Signal that the thread has terminated.
			close(stoppedCh)
		}()
		return nil
	})
	g.Expect(err).To(BeNil())

	// Stop the worker.
	var preStopTime time.Time
	var postStopTime time.Time
	preStopFn := func() {
		preStopTime = time.Now()

		// Sleep for a bit to force the stop time to be later.
		time.Sleep(10 * time.Millisecond)
	}
	postStopFn := func() {
		postStopTime = time.Now()
	}
	w.Stop(preStopFn, postStopFn)
	if !preStopTime.Before(stopTime) || !stopTime.Before(postStopTime) {
		t.Fatalf("unexpected time ordering: pre: %v, stopped: %v, post: %v", preStopTime, stopTime, postStopTime)
	}
}

func newWorker() *util.Worker {
	return util.NewWorker("testWorker", nil)
}
