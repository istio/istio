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

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

// StatusReporter reports diagnostic messages.
type StatusReporter interface {
	Report(msgs diag.Messages)
}

// InMemoryStatusReporter is an in-memory implementation of status reporter
type InMemoryStatusReporter struct {
	mu   sync.Mutex
	msgs diag.Messages

	readyCh chan struct{}
}

// Report implements StatusReporter
func (s *InMemoryStatusReporter) Report(msgs diag.Messages) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = msgs
	if s.readyCh != nil {
		close(s.readyCh)
		s.readyCh = nil
	}
}

// Get returns the current diagnostic messages
func (s *InMemoryStatusReporter) Get() diag.Messages {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msgs
}

// WaitForReport blocks the calling goroutine until Report is called, or cancel channel is closed. Returns true
// if a report is available.
func (s *InMemoryStatusReporter) WaitForReport(cancel chan struct{}) bool {
	s.mu.Lock()
	if s.readyCh == nil {
		s.readyCh = make(chan struct{})
	}
	ch := s.readyCh
	s.mu.Unlock()

	select {
	case <-ch:
		return true
	case <-cancel:
		return false
	}
}