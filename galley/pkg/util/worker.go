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

package util

import (
	"context"
	"fmt"
	"sync"

	"istio.io/istio/pkg/log"
)

// Worker is a utility to help manage the lifecycle of a worker thread.
type Worker struct {
	// name of the worker thread used for logging.
	name string

	// scope is the logging scope to be used for log messages.
	scope log.Scope

	// Lock for changing the running state of the source
	stateLock sync.Mutex

	// started indicates that the user has started this source (via Start).
	started bool

	// stopped indicates that the user has stopped this source (via Stop).
	stopped bool

	// ctx is the context used to run the worker thread.
	ctx context.Context

	// ctxCancel is the ctxCancel function for ctx.
	ctxCancel context.CancelFunc

	// stoppedCh is used to indicate that the k8s informer has exited.
	stoppedCh chan struct{}
}

// NewWorker creates a new worker with the given name and logging scope.
func NewWorker(name string, scope *log.Scope) *Worker {
	if scope == nil {
		scope = log.FindScope(log.DefaultScopeName)
	}
	worker := &Worker{
		name:      name,
		scope:     *scope,
		stoppedCh: make(chan struct{}, 1),
	}

	// Create the run context.
	worker.ctx, worker.ctxCancel = context.WithCancel(context.Background())
	return worker
}

// Start the worker thread via the provided lambda. The runFn lambda is run in a
// go routine and is provided a context to trigger the exit of the function. If
// this Worker was already started, returns an error.
func (w *Worker) Start(setupFn func() error, runFn func(c context.Context)) error {
	w.stateLock.Lock()
	defer w.stateLock.Unlock()

	if w.started {
		return fmt.Errorf("%s already started", w.name)
	}
	w.started = true

	if setupFn != nil {
		if err := setupFn(); err != nil {
			return err
		}
	}

	go func() {
		// Run the user-supplied lambda
		runFn(w.ctx)

		// Signal back to the Stop method that the worker thread has stopped.
		close(w.stoppedCh)
	}()

	return nil
}

// Stop the worker thread and waits for it to exit gracefully.
func (w *Worker) Stop() {
	w.stateLock.Lock()
	if !w.started {
		w.scope.Warnf("%s was never started", w.name)
		w.stateLock.Unlock()
		return
	}
	if w.stopped {
		w.scope.Warnf("%s already stopped", w.name)
		w.stateLock.Unlock()
		return
	}
	w.stopped = true
	w.stateLock.Unlock()

	// Signal to the worker thread that we're shutting down.
	w.ctxCancel()

	// Wait for the worker thread to exit.
	<-w.stoppedCh
}
