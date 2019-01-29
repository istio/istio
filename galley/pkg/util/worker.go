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

	// stopCh is used to trigger the k8s informer to stop.
	stopCh chan struct{}

	// stoppedCh is used to indicate that the k8s informer has exited.
	stoppedCh chan struct{}
}

// NewWorker creates a new worker with the given name and logging scope.
func NewWorker(name string, scope *log.Scope) *Worker {
	if scope == nil {
		scope = log.FindScope(log.DefaultScopeName)
	}
	return &Worker{
		name:      name,
		scope:     *scope,
		stopCh:    make(chan struct{}, 1),
		stoppedCh: make(chan struct{}, 1),
	}
}

// Start the worker thread via the provided function. The startWorkerThread function is
// provided two control channels. When the user closes this worker, stopCh is closed to
// signal that the worker thread should begin shutting down. Upon exiting, the worker
// thread should in turn close stoppedCh to signal back to the worker that the thread
// has terminated.
func (w *Worker) Start(startWorkerThread func(stopCh chan struct{}, stoppedCh chan struct{}) error) error {
	w.stateLock.Lock()
	defer w.stateLock.Unlock()

	if w.started {
		return fmt.Errorf("%s already started", w.name)
	}

	err := startWorkerThread(w.stopCh, w.stoppedCh)
	if err == nil {
		w.started = true
	}
	return err
}

// Stop the worker thread. Once the worker thread has terminated, the provided cleanup
// function is executed.
func (w *Worker) Stop(preStopFn func(), postStopFn func()) {
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

	// Perform custom pre-stop processing.
	if preStopFn != nil {
		preStopFn()
	}

	// Signal to the worker thread that we're shutting down.
	close(w.stopCh)

	// Wait for the worker thread to exit.
	<-w.stoppedCh

	// Perform custom post-stop processing.
	if postStopFn != nil {
		postStopFn()
	}
}
