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
	inactive  = sessionState("inactive")
	booting   = sessionState("booting")
	executing = sessionState("executing")
)

type session struct {
	mu      sync.Mutex
	id      int32
	options RuntimeOptions
	meshCfg *v1alpha1.MeshConfig
	buffer  *event.Buffer
	state   sessionState
	done    chan struct{}
}

func newSession(id int32, o RuntimeOptions) (*session, chan struct{}) {
	s := &session{
		id:      id,
		options: o,
		buffer:  event.NewBuffer(),
		state:   inactive,
		done:    make(chan struct{}),
	}

	s.buffer.Dispatch(o.Processor)
	return s, s.done
}

func (s *session) handle(e event.Event) {
	if e.Kind == event.Reset {
		s.mu.Lock()
		switch s.state {
		case inactive:
			s.mu.Unlock()

		default: // nolint:gocritic nolint:stylecheck
			scope.Warnf("Invalid session state: %v", s.state)
			fallthrough

		case booting, executing:
			s.state = inactive
			go s.stop()
			s.mu.Unlock()
		}
		return
	}

	s.buffer.Handle(e)

	if e.Source == meshcfg.IstioMeshconfig {
		s.handleMeshEvent(e)
	}
}

func (s *session) start() {
	s.mu.Lock()
	if s.state != inactive {
		panic(fmt.Sprintf("invalid state: %s (expecting inactive)", s.state))
	}
	s.state = booting
	s.mu.Unlock()

	for _, src := range s.options.Sources {
		src.Start()
	}
}

func (s *session) stop() {
	s.mu.Lock()
	s.state = inactive
	s.mu.Unlock()

	s.buffer.Stop()
	s.options.Processor.Stop()
	for _, s := range s.options.Sources {
		s.Stop()
	}
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
}

func (s *session) handleMeshEvent(e event.Event) {
	s.mu.Lock()

	switch s.state {
	case inactive:
		s.mu.Unlock()
		return

	case executing:
		scope.Infof("session.handleMeshEvent: Mesh event received during running state, restarting: %+v", e)
		s.state = inactive
		go s.stop()
		s.mu.Unlock()
		return

	default: // nolint:gocritic nolint:stylecheck
		scope.Warnf("Runtime.handleMeshEvent: mesh event in unsupported state '%v': %+v", s.state, e)
		s.mu.Unlock()
		return

	case booting:
		// fallthrough. Lock is still held.
	}

	switch e.Kind {
	case event.Added, event.Updated:
		scope.Infof("Runtime.handleMeshEvent: received an add/update mesh config event: %v", e)
		s.meshCfg = proto.Clone(e.Entry.Item).(*v1alpha1.MeshConfig)

	case event.Deleted:
		scope.Infof("Runtime.handleMeshEvent: received a delete mesh config event: %v", e)
		s.meshCfg = meshcfg.Default()

	case event.FullSync:
		scope.Infof("Runtime.handleMeshEvent: Mesh config is fully synced in '%v' state, transitioning to '%v': %+v",
			s.state, executing, e)

		scope.Debugf("Runtime.handleMeshEvent: %v => %v", s.state, executing)
		s.state = executing

		o := ProcessorOptions{
			DomainSuffix: s.options.DomainSuffix,
			MeshConfig:   proto.Clone(s.meshCfg).(*v1alpha1.MeshConfig),
		}
		s.options.Processor.Start(o)
		go s.buffer.Process()

	default:
		scope.Errorf("Runtime.handleMeshEvent: unrecognized event kind: %v", e.Kind)
	}

	s.mu.Unlock()
}
