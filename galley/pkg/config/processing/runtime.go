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

package processing

import (
	"sync"
	"sync/atomic"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
)

// RuntimeOptions is options for Runtime
type RuntimeOptions struct {
	Source            event.Source
	ProcessorProvider ProcessorProvider
	DomainSuffix      string
}

// Clone returns a cloned copy of the RuntimeOptions.
func (o RuntimeOptions) Clone() RuntimeOptions {
	return o
}

// Runtime is the top-level config processing machinery. Through runtime options, it takes in a set of Sources and
// a Processor. Once started, Runtime will go through a startup phase, where it waits for MeshConfig to arrive before
// starting the Processor. If, the Runtime receives any event.RESET events, or if there is a change to the MeshConfig,
// then the Runtime will stop the processor and sources and will restart them again.
//
// Internally, Runtime uses the session type to implement this stateful behavior. The session handles state transitions
// and is responsible for starting/stopping the Sources, Processors, in the correct order.
type Runtime struct { // nolint:maligned
	mu sync.RWMutex

	// counter for session id. The current value reflects the processing session's id.
	sessionIDCtr int32

	// runtime options that was passed as parameters to the command-line.
	options RuntimeOptions

	// stopCh is used to send stop signal completion to the background go-routine.
	stopCh chan struct{}

	// wg is used to synchronize the completion of Stop call with the completion of the background
	// go routine.
	wg      sync.WaitGroup
	session atomic.Value
}

// NewRuntime returns a new instance of a processing.Runtime.
func NewRuntime(o RuntimeOptions) *Runtime {

	r := &Runtime{
		options: o.Clone(),
	}

	h := event.HandlerFromFn(r.handle)
	o.Source.Dispatch(h)

	return r
}

// Start the Runtime
func (r *Runtime) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopCh != nil {
		scope.Processing.Warnf("Runtime.Start: already started")
		return
	}
	r.stopCh = make(chan struct{})

	r.wg.Add(1)
	startedCh := make(chan struct{})
	go r.run(startedCh, r.stopCh)
	<-startedCh
}

// Stop the Runtime
func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopCh == nil {
		return
	}
	close(r.stopCh)
	r.wg.Wait()

	r.stopCh = nil
}

// currentSessionID is a numeric identifier of internal Runtime state. It is used for debugging purposes.
func (r *Runtime) currentSessionID() int32 {
	var id int32
	se := r.session.Load()
	if se != nil {
		s := se.(*session)
		id = s.id
	}
	return id
}

// currentSessionState is the state of the internal Runtime state. It is used for debugging purposes.
func (r *Runtime) currentSessionState() sessionState {
	var state sessionState
	se := r.session.Load()
	if se != nil {
		s := se.(*session)
		state = s.getState()
	}
	return state
}

func (r *Runtime) run(startedCh, stopCh chan struct{}) {
loop:
	for {
		sid := atomic.AddInt32(&r.sessionIDCtr, 1)
		scope.Processing.Infof("Runtime.run: Starting new session id:%d", sid)
		se, done := newSession(sid, r.options)
		r.session.Store(se)
		se.start()

		if startedCh != nil {
			close(startedCh)
			startedCh = nil
		}

		select {
		case <-done:
			scope.Processing.Infof("Runtime.run: Completing session: id:%d", sid)

		case <-stopCh:
			scope.Processing.Infof("Runtime.run: Stopping session: id%d", sid)
			se.stop()
			break loop
		}
	}

	r.wg.Done()
	scope.Processing.Info("Runtime.run: Exiting...")
}

func (r *Runtime) handle(e event.Event) {
	se := r.session.Load()
	if se == nil {
		return
	}

	s := se.(*session)
	s.handle(e)
}
