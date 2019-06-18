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

package apiserver

import (
	"sync"

	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
)

var scope = log.RegisterScope("source", "", 0)

// Source is an implementation of processing.KubeSource
type Source struct {
	mu       sync.Mutex
	options  Options
	started  bool
	watchers map[collection.Name]*watcher
}

var _ event.Source = &Source{}

// New returns a new kube.Source.
func New(o Options) (*Source, error) {
	s := &Source{
		watchers: make(map[collection.Name]*watcher),
		options:  o,
	}

	p := rt.NewProvider(o.Client, o.ResyncPeriod)

	scope.Info("creating sources for Kubernetes resources")
	for i, r := range o.Resources {
		a := p.GetAdapter(r)

		scope.Infof("[%d]", i)
		scope.Infof("  Source:      %s", r.CanonicalResourceName())
		scope.Infof("  Name:  		 %s", r.Collection)
		scope.Infof("  Built-in:    %v", a.IsBuiltIn())

		col := newWatcher(r, a)
		s.watchers[r.Collection.Name] = col
	}

	return s, nil
}

// Dispatch implements processor.Source
func (s *Source) Dispatch(h event.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.watchers {
		c.dispatch(h)
	}
}

// Start implements processor.Source
func (s *Source) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		scope.Warn("Source.Start: already started")
		return
	}
	s.started = true

	for c, w := range s.watchers {
		scope.Debuga("Source.Start: starting watcher: ", c)
		w.start()
	}
}

// Stop implements processor.Source
func (s *Source) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		scope.Warn("Source.Stop: Already stopped")
		return
	}

	s.stop()
}

func (s *Source) stop() {
	for c, w := range s.watchers {
		scope.Debuga("Source.Stop: stopping watcher: ", c)
		w.stop()
	}
	s.started = false
}
