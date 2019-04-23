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
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/util"
)

func TestStopBeforeStartShouldNotPanic(t *testing.T) {
	w := newWorker()
	w.Stop()
}

func TestSetupError(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	expected := errors.New("fake error")
	setupFn := func() error {
		return expected
	}
	startFn := func(ctx context.Context) {
		<-ctx.Done()
	}

	err := w.Start(setupFn, startFn)
	g.Expect(err).To(Equal(expected))
}

func TestStartTwiceShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	startFn := func(ctx context.Context) {
		<-ctx.Done()
	}

	err := w.Start(nil, startFn)
	g.Expect(err).To(BeNil())
	defer w.Stop()

	// Second call should fail
	err = w.Start(nil, startFn)
	g.Expect(err).ToNot(BeNil())
}

func TestStopWaitsForRunToExit(t *testing.T) {
	g := NewGomegaWithT(t)

	w := newWorker()

	count := 0

	var workerStopTime time.Time
	startFn := func(ctx context.Context) {
		count++
		<-ctx.Done()

		workerStopTime = time.Now()

		// Wait a bit to make sure that the next event occurs after.
		time.Sleep(10 * time.Millisecond)
	}

	err := w.Start(nil, startFn)
	g.Expect(err).To(BeNil())

	// Now stop it.
	w.Stop()
	stopReturned := time.Now()

	g.Expect(count).To(Equal(1))
	if !workerStopTime.Before(stopReturned) {
		t.Fatalf("unexpected time ordering: worker stopped: %v, Stop() returned: %v",
			workerStopTime, stopReturned)
	}
}

func newWorker() *util.Worker {
	return util.NewWorker("testWorker", nil)
}
