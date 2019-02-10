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

package runtime

import (
	"testing"

	"istio.io/istio/pkg/test/framework2/components/environment"
)

// Instance for the test environment.
type Instance struct {
	Suite *SuiteContext
}

// New returns a new runtime instance.
func New(s *Settings, fn environment.FactoryFn) (*Instance, error) {
	ctx, err := newSuiteContext(s, fn)
	if err != nil {
		return nil, err
	}
	return &Instance{
		Suite: ctx,
	}, nil
}

// RunTest executes a test.
func (i *Instance) RunTest(t *testing.T, fn func(*TestContext)) {
	ctx := newTestContext(i.Suite, nil, t)
	defer ctx.done()

	fn(ctx)
}

// Dump state for all allocated resources.
func (i *Instance) Dump() {
	i.Suite.globalScope.dump()
}

// Close implements io.Closer
func (i *Instance) Close() error {
	return i.Suite.globalScope.done(i.Suite.settings.NoCleanup)
}
