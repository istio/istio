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

	"istio.io/istio/pkg/test/framework/components/environment/api"
	"istio.io/istio/pkg/test/framework/core"
	"istio.io/istio/pkg/test/framework/label"
)

// Instance for the test environment.
type Instance struct {
	context *suiteContext
}

// New returns a new runtime instance.
func New(s *core.Settings, fn api.FactoryFn, labels label.Set) (*Instance, error) {
	ctx, err := newSuiteContext(s, fn, labels)
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
func (i *Instance) SuiteContext() *suiteContext { // nolint:golint
	return i.context
}

// NewTestContext creates and returns a new testContext
func (i *Instance) NewTestContext(t *testing.T, parentContext *testContext, labels label.Set) *testContext { // nolint:golint
	var parentScope *scope
	if parentContext != nil {
		parentScope = parentContext.scope
	}
	return newTestContext(t, i.context, parentScope, labels)
}

// Close implements io.Closer
func (i *Instance) Close() error {
	return i.context.globalScope.done(i.context.settings.NoCleanup)
}
