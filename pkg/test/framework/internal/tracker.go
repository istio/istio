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

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/dependency"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

// Tracker keeps track of the state information for dependencies
type Tracker map[dependency.Instance]interface{}

// Initialize a test dependency and start tracking it.
func (t Tracker) Initialize(ctx *TestContext, env Environment, dep dependency.Instance) error {
	if _, ok := t[dep]; ok {
		scope.Debugf("Dependency already initialized: %v", dep)
		return nil
	}

	s, err := env.InitializeDependency(ctx, dep)
	if err != nil {
		scope.Debugf("Error initializing dependency '%v': %v", dep, err)
		return err
	}

	t[dep] = s
	return nil
}

// All returns all tracked resources.
func (t Tracker) All() []interface{} {
	r := make([]interface{}, 0, len(t))
	for _, s := range t {
		r = append(r, s)
	}

	return r
}

// Reset the all Resettable resources.
func (t Tracker) Reset(ctx *TestContext) error {
	var er error

	for k, v := range t {
		if cl, ok := v.(Resettable); ok {
			scope.Debugf("Resetting state for dependency: %s", k)
			if err := cl.Reset(ctx); err != nil {
				scope.Errorf("Error resetting dependency state: %s: %v", k, err)
				er = multierr.Append(er, err)
			}
		}
	}

	return er
}

// Cleanup closes all resources that implement io.Closer
func (t Tracker) Cleanup() {
	for k, v := range t {
		if cl, ok := v.(io.Closer); ok {
			scope.Debugf("Cleaning up state for dependency: %s", k)
			if err := cl.Close(); err != nil {
				scope.Errorf("Error cleaning up dependency state: %s: %v", k, err)
			}
		}
	}

	for k := range t {
		delete(t, k)
	}
}
