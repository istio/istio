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
	"fmt"
	"sync"

	"github.com/gogo/protobuf/proto"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collections"
)

type sessionState string

const (
	// The session is inactive. This is both the initial, and the terminal state of the session.
	// Allowed transitions are: starting
	inactive = sessionState("inactive")

	// The session is starting up. In this phase, the sources are being initialized. The starting session is an explicit
	// state, since it is possible to get lifecycle events during the startup of sources. Having an explicit state for
	// startup enables handling such lifecycle events appropriately.
	// Allowed transitions are: buffering, terminating
	starting = sessionState("starting")

	// The session is buffering events until a mesh configuration arrives.
	// Allowed transitions are: processing, terminating
	buffering = sessionState("buffering")

	// The session is in full execution mode, processing events.
	// Allowed transitions are: terminating
	processing = sessionState("processing")

	// The session is terminating. It will ignore all incoming events, while processors & sources are being stopped.
	// Allowed transitions are: inactive
	terminating = sessionState("terminating")
)

// session represents a config processing session. It is a stateful controller type whose main responsibility is to
// manage state transitions and react to the events that impact lifecycle.
//
// A session starts with an external request (through the start() method, called by Runtime) which puts the session into
// the "starting" state. During this phase, the Sources are started, and the events from Sources  start to come in. Once
// all sources are started, the session transitions to the "buffering" state.
//
// In "buffering" (and also in "starting") state, the incoming events are selectively buffered, until a usable mesh
// configuration is received. Once received, the buffered events start getting processed.
//
// The main difference between buffering & starting states is how life-cycle events (such as a stop() call from
// Runtime, or a received reset event) are handled. In starting state, the cleanup is performed right within the context
// of the startup call.
//
// Once a mesh config is received, the state transitions to the "processing" state, where the processor is initialized
// and buffered events starts getting processed. This is a steady-state, and persists until a life-cycle event occurs
// (i.e. an explicit stop call from Runtime, a Reset event, or a change in mesh config). Once such a life-cycle event
// occurs, the state transitions to "terminating", and teardown operations take place (i.e. stop Processor, stop
// Sources etc.).
type session struct { // nolint:maligned
	mu sync.Mutex

	id      int32
	options RuntimeOptions
	buffer  *event.Buffer

	state sessionState

	// mesh config state
	meshCfg    *v1alpha1.MeshConfig
	meshSynced bool

	processor event.Processor
	doneCh    chan struct{}
}

// newSession creates a new config processing session state. It returns the session, as well as a channel
// that will be closed upon termination of the session.
func newSession(id int32, o RuntimeOptions) (*session, chan struct{}) {
	s := &session{
		id:      id,
		options: o,
		buffer:  event.NewBuffer(),
		state:   inactive,
		doneCh:  make(chan struct{}),
	}

	return s, s.doneCh
}

// start the session. This must be called when state == inactive.
func (s *session) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != inactive {
		panic(fmt.Sprintf("invalid state: %s (expecting inactive)", s.state))
	}
	s.transitionTo(starting)

	go s.startSources()
}

func (s *session) startSources() {
	scope.Processing.Debug("session starting sources...")
	// start source after relinquishing lock. This avoids deadlocks.
	s.options.Source.Start()

	scope.Processing.Debugf("session source start complete")

	// check the state again. During startup we might have received mesh config, or got signaled for stop.
	var terminate bool
	s.mu.Lock()
	switch s.state {
	case starting:
		// This is the expected state. Depending on whether we received mesh config or not we can transition to the
		// buffering, or processing states.
		s.transitionTo(buffering)
		if s.meshSynced {
			s.startProcessing()
		}

	case terminating:
		// stop was called during startup. There is nothing we can do, simply exit.
		terminate = true

	default:
		panic(fmt.Sprintf("session.start: unexpected state during session startup: %v", s.state))
	}
	s.mu.Unlock()

	if terminate {
		s.terminate()
	}
}

func (s *session) stop() {
	scope.Processing.Debug("session.stop()")

	var terminate bool
	s.mu.Lock()
	switch s.state {
	case starting:
		// set the state to terminating and let the startup code complete startup steps and deal with termination.
		s.transitionTo(terminating)

	case buffering, processing:
		s.transitionTo(terminating)
		terminate = true

	default:
		panic(fmt.Errorf("session.stop: Invalid state: %v", s.state))
	}
	s.mu.Unlock()
	if terminate {
		s.terminate()
	}
}

func (s *session) terminate() {
	// must be called outside lock.
	s.mu.Lock()
	if s.state != terminating {
		panic(fmt.Sprintf("invalid state: %s (expecting terminating)", s.state))
	}
	s.mu.Unlock()

	scope.Processing.Debug("session.terminate: stopping buffer...")
	s.buffer.Stop()
	scope.Processing.Debug("session.terminate: stopping processor...")
	if s.processor != nil {
		s.processor.Stop()
	}
	scope.Processing.Debug("session.terminate: stopping sources...")
	s.options.Source.Stop()

	scope.Processing.Debug("session.terminate: signaling session termination...")
	s.mu.Lock()
	if s.doneCh != nil {
		close(s.doneCh)
		s.doneCh = nil
	}
	s.transitionTo(inactive)
	s.mu.Unlock()
}

func (s *session) startProcessing() {
	// must be called under lock.
	if s.state != buffering {
		panic(fmt.Sprintf("invalid state: %s (expecting buffering)", s.state))
	}

	// immediately transition to the processing state
	o := ProcessorOptions{
		DomainSuffix: s.options.DomainSuffix,
		MeshConfig:   proto.Clone(s.meshCfg).(*v1alpha1.MeshConfig),
	}
	s.processor = s.options.ProcessorProvider(o)
	s.buffer.Dispatch(s.processor)
	s.processor.Start()
	s.transitionTo(processing)
	go s.buffer.Process()
}

func (s *session) handle(e event.Event) {
	// Check the event kind first to avoid excessive locking.
	if e.Kind != event.Reset {
		s.buffer.Handle(e)

		if e.SourceName() == collections.IstioMeshV1Alpha1MeshConfig.Name() {
			s.handleMeshEvent(e)
		}
		return
	}

	// Handle the reset event
	s.mu.Lock()
	switch s.state {
	case inactive, terminating:
		// nothing to do

	case starting:
		// set the state to terminating and let the startup code complete startup steps and deal with termination.
		s.transitionTo(terminating)

	case buffering, processing:
		s.transitionTo(terminating)
		go s.terminate()

	default:
		panic(fmt.Errorf("session.handle: invalid session state: %v", s.state))
	}
	s.mu.Unlock()
}

func (s *session) handleMeshEvent(e event.Event) {
	s.mu.Lock()

	switch s.state {
	case inactive, terminating:
		// nothing to do

	case processing:
		scope.Processing.Infof("session.handleMeshEvent: Mesh event received during running state, restarting: %+v", e)
		s.transitionTo(terminating)
		go s.terminate()

	case starting:
		scope.Processing.Infof("session.handleMeshEvent: Received initial mesh event, applying it: %+v", e)
		s.applyMeshEvent(e)

	case buffering:
		s.applyMeshEvent(e)
		if s.meshSynced {
			s.startProcessing()
		}

	default:
		panic(fmt.Errorf("session.handleMeshEvent: mesh event in unsupported state '%v': %+v", s.state, e))
	}

	s.mu.Unlock()
}

func (s *session) applyMeshEvent(e event.Event) {
	// Apply the meshconfig changes directly to the internal state.
	switch e.Kind {
	case event.Added, event.Updated:
		scope.Processing.Infof("session.handleMeshEvent: received an add/update mesh config event: %v", e)
		s.meshCfg = proto.Clone(e.Resource.Message).(*v1alpha1.MeshConfig)
	case event.Deleted:
		scope.Processing.Infof("session.handleMeshEvent: received a delete mesh config event: %v", e)
		s.meshCfg = mesh.DefaultMeshConfig()
	case event.FullSync:
		scope.Processing.Infof("session.applyMeshEvent meshSynced: %v => %v", s.meshSynced, true)
		s.meshSynced = true

	// reset case is already handled by the time call arrives here.

	default:
		panic(fmt.Errorf("session.handleMeshEvent: unrecognized event kind: %v", e.Kind))
	}
}

func (s *session) transitionTo(st sessionState) {
	scope.Processing.Infof("session[%d] %q => %q", s.id, s.state, st)
	s.state = st
}

// getState returns the state of session. This is useful for testing/debugging purposes.
func (s *session) getState() sessionState {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	return st
}
