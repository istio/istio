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
	"fmt"
	"sync"

	"github.com/gogo/protobuf/proto"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
)

type sessionState string

const (
	// The session is inactive. This is both the initial, and the terminal state of the session.
	// allowed transitions are: starting
	inactive = sessionState("inactive")

	// The session is starting up. In this phase, the sources are being initialized.
	// allowed transitions are: buffering, terminating
	starting = sessionState("starting")

	// The session is buffering events until a mesh configuration can arrive.
	// allowed transitions are: processing, terminating
	buffering = sessionState("buffering")

	// The session is in full execution mode, processing events.
	// allowed transitions are: terminating
	processing = sessionState("processing")

	// The session is terminating. It will ignore all incoming events, while processors & sources are being stopped.
	// allowed transitions are: inactive
	terminating = sessionState("terminating")
)

type session struct {
	mu         sync.Mutex
	id         int32
	options    RuntimeOptions
	meshCfg    *v1alpha1.MeshConfig
	meshSynced bool
	buffer     *event.Buffer
	state      sessionState

	doneCh chan struct{}
}

// newSession creates a new config processing session state. It returns the session, as well as a channel
// that will be closed upon completion of the session activity.
func newSession(id int32, o RuntimeOptions) (*session, chan struct{}) {
	s := &session{
		id:      id,
		options: o,
		buffer:  event.NewBuffer(),
		state:   inactive,
		doneCh:  make(chan struct{}),
	}

	s.buffer.Dispatch(o.Processor)
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
	s.state = starting

	go s.startSources()
}

func (s *session) startSources() {
	scope.Debug("session starting sources...")
	// start sources after relinquishing lock. This avoids deadlocks.
	for _, src := range s.options.Sources {
		src.Start()
	}
	scope.Debugf("session source start complete")

	// check the state again. During startup we might have received mesh config, or got signalled for stop.
	var terminate bool
	s.mu.Lock()
	switch s.state {
	case starting:
		// This is the expected state. Depending on whether we received mesh config, or not we can transition to the
		// next state.
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
	scope.Debug("session.stop()")

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
		scope.Warnf("session.stop: Invalid state: %v", s.state)
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

	scope.Debug("session.terminate: stopping buffer...")
	s.buffer.Stop()
	scope.Debug("session.terminate: stopping processor...")
	s.options.Processor.Stop()
	scope.Debug("session.terminate: stopping sources...")
	for _, s := range s.options.Sources {
		s.Stop()
	}

	scope.Debug("session.terminate: signalling session termination...")
	s.mu.Lock()
	if s.doneCh != nil {
		close(s.doneCh)
		s.doneCh = nil
	}
	s.state = inactive
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
	s.options.Processor.Start(o)
	s.transitionTo(processing)
	go s.buffer.Process()
}

func (s *session) handle(e event.Event) {
	// Check the event kind first to avoid excessive locking.
	if e.Kind != event.Reset {
		s.buffer.Handle(e)

		if e.Source == meshcfg.IstioMeshconfig {
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
		s.state = terminating

	case buffering, processing:
		s.transitionTo(terminating)
		go s.terminate()

	default:
		scope.Warnf("Invalid session state: %v", s.state)
	}
	s.mu.Unlock()
}

func (s *session) handleMeshEvent(e event.Event) {
	s.mu.Lock()

	switch s.state {
	case inactive, terminating:
		// nothing to do

	case processing:
		scope.Infof("session.handleMeshEvent: Mesh event received during running state, restarting: %+v", e)
		s.transitionTo(terminating)
		go s.terminate()

	case starting:
		s.applyMeshEvent(e)

	case buffering:
		s.applyMeshEvent(e)
		if s.meshSynced {
			s.startProcessing()
		}

	default:
		scope.Warnf("session.handleMeshEvent: mesh event in unsupported state '%v': %+v", s.state, e)
	}

	s.mu.Unlock()
}

func (s *session) applyMeshEvent(e event.Event) {
	// Apply the meshconfig changes directly to the internal state.
	switch e.Kind {
	case event.Added, event.Updated:
		scope.Debugf("session.handleMeshEvent: received an add/update mesh config event: %v", e)
		s.meshCfg = proto.Clone(e.Entry.Item).(*v1alpha1.MeshConfig)
	case event.Deleted:
		scope.Debugf("session.handleMeshEvent: received a delete mesh config event: %v", e)
		s.meshCfg = meshcfg.Default()
	case event.FullSync:
		scope.Infof("session.applyMeshEvent meshSynced: %v => %v", s.meshSynced, true)
		s.meshSynced = true

	// reset case is already handled by the time call arrives here.

	default:
		scope.Errorf("Runtime.handleMeshEvent: unrecognized event kind: %v", e.Kind)
	}
}

func (s *session) transitionTo(st sessionState) {
	scope.Infof("session[%d] %q => %q", s.id, s.state, st)
	s.state = st
}
