//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package internal

import (
	"io"

	"go.uber.org/multierr"

	"fmt"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/component"
	"istio.io/istio/pkg/test/framework/components/registry"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

// Tracker keeps track of the state information for dependencies
type Tracker struct {
	depMap   map[dependency.Instance]interface{}
	registry *registry.Registry
}

func newTracker(registry *registry.Registry) *Tracker {
	return &Tracker{
		depMap:   make(map[dependency.Instance]interface{}),
		registry: registry,
	}
}

// Initialize a test dependency and start tracking it.
func (t *Tracker) Initialize(ctx environment.ComponentContext, c component.Component) (interface{}, error) {
	id := c.ID()
	if s, ok := t.depMap[id]; ok {
		// Already initialized.
		return s, nil
	}

	// Make sure all dependencies of the component are initialized first.
	depMap := make(map[dependency.Instance]interface{})
	for _, depID := range c.Requires() {
		depComp, ok := t.registry.Get(depID)
		if !ok {
			return nil, fmt.Errorf("unable to resolve dependency %s for component %s", depID, id)
		}

		// TODO(nmittler): We might want to protect against circular dependencies.
		s, err := t.Initialize(ctx, depComp)
		if err != nil {
			return nil, err
		}

		depMap[depID] = s
	}

	s, err := c.Init(ctx, depMap)
	if err != nil {
		return nil, err
	}

	t.depMap[id] = s
	return s, nil
}

// Get the tracked resource with the given ID.
func (t *Tracker) Get(id dependency.Instance) (interface{}, bool) {
	s, ok := t.depMap[id]
	return s, ok
}

// All returns all tracked resources.
func (t *Tracker) All() []interface{} {
	r := make([]interface{}, 0, len(t.depMap))
	for _, s := range t.depMap {
		r = append(r, s)
	}

	return r
}

// Reset the all Resettable resources.
func (t *Tracker) Reset() error {
	var er error

	for k, v := range t.depMap {
		if cl, ok := v.(Resettable); ok {
			scope.Debugf("Resetting state for dependency: %s", k)
			if err := cl.Reset(); err != nil {
				scope.Errorf("Error resetting dependency state: %s: %v", k, err)
				er = multierr.Append(er, err)
			}
		}
	}

	return er
}

// Cleanup closes all resources that implement io.Closer
func (t *Tracker) Cleanup() {
	for k, v := range t.depMap {
		if cl, ok := v.(io.Closer); ok {
			scope.Debugf("Cleaning up state for dependency: %s", k)
			if err := cl.Close(); err != nil {
				scope.Errorf("Error cleaning up dependency state: %s: %v", k, err)
			}
		}
	}

	for k := range t.depMap {
		delete(t.depMap, k)
	}
}
