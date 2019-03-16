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
	"istio.io/istio/pkg/test/framework2/core"
)

// Instance for the test environment.
type Instance struct {
	context *suiteContext
}

// New returns a new runtime instance.
func New(s *core.Settings, fn environment.FactoryFn) (*Instance, error) {
	ctx, err := newSuiteContext(s, fn)
	if err != nil {
		return nil, err
	}
	return &Instance{
		context: ctx,
	}, nil
}

// Dump state for all allocated resources.
func (i *Instance) Dump() {
	i.context.globalScope.dump()
}

// suiteContext returns the suiteContext.
func (i *Instance) SuiteContext() core.SuiteContext {
	return i.context
}

// NewTestContext creates and returns a new testContext
func (i *Instance) NewTestContext(parentContext core.TestContext, t *testing.T) core.TestContext {
	var parentScope *scope
	if parentContext != nil {
		pc, ok := parentContext.(*testContext)
		if !ok {
			t.Fatal("NewTestContext: unexpected testContext implementation was passed.")
		}
		parentScope = pc.scope
	}
	return newTestContext(i.context, parentScope, t)
}

// Close implements io.Closer
func (i *Instance) Close() error {
	return i.context.globalScope.done(i.context.settings.NoCleanup)
}
