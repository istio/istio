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

package processing

import (
	"sync"
	"sync/atomic"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/event"
)

// ProcessorOptions are options that are passed to event.Processors during startup.
type ProcessorOptions struct {
	MeshConfig   *v1alpha1.MeshConfig
	DomainSuffix string
}

// RuntimeOptions is options for Runtime
type RuntimeOptions struct {
	Sources      []event.Source
	Processor    event.Processor
	DomainSuffix string
}

// Clone returns a cloned copy of the RuntimeOptions.
func (o *RuntimeOptions) Clone() RuntimeOptions {
	sources := make([]event.Source, len(o.Sources))
	copy(sources, o.Sources)

	return RuntimeOptions{
		Sources:      sources,
		Processor:    o.Processor,
		DomainSuffix: o.DomainSuffix,
	}
}

// Runtime is the config processing runtime.
type Runtime struct {
	mu sync.RWMutex

	// counter for session id. The current value reflects the processing session's id.
	sessionIDCtr int32

	// runtime options that was passed as parameters to the command-line.
	options RuntimeOptions

	// stopCh is used send stop signal completion to the background go-routine.
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
	for _, s := range o.Sources {
		s.Dispatch(h)
	}
	return r
}

// Start the Processor
func (r *Runtime) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopCh != nil {
		scope.Warnf("Runtime.Start: already started")
		return
	}
	r.stopCh = make(chan struct{})

	r.wg.Add(1)
	go r.run(r.stopCh)
}

// Stop the Processor
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

// CurrentSessionID is a numeric identifier of internal processor state. It is used for debugging purposes.
func (r *Runtime) CurrentSessionID() int32 {
	se := r.session.Load()
	if se == nil {
		return 0
	}
	s := se.(*session)
	return s.id
}

func (r *Runtime) run(stopCh chan struct{}) {
loop:
	for {
		sid := atomic.AddInt32(&r.sessionIDCtr, 1)
		se, done := newSession(sid, r.options)
		r.session.Store(se)
		se.start()

		select {
		case <-done:

		case <-stopCh:
			se.stop()
			break loop
		}
	}

	r.wg.Done()
}

func (r *Runtime) handle(e event.Event) {
	se := r.session.Load()
	if se == nil {
		return
	}

	s := se.(*session)
	s.handle(e)
}
