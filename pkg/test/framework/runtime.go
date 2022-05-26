//  Copyright Istio Authors
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

package framework

import (
	"testing"

	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ resource.Dumper = &runtime{}

// runtime for the test environment.
type runtime struct {
	context *suiteContext
}

// newRuntime returns a new runtime instance.
func newRuntime(s *resource.Settings, fn resource.EnvironmentFactory, labels label.Set) (*runtime, error) {
	ctx, err := newSuiteContext(s, fn, labels)
	if err != nil {
		return nil, err
	}
	return &runtime{
		context: ctx,
	}, nil
}

// Dump state for all allocated resources.
func (i *runtime) Dump(ctx resource.Context) {
	i.DumpCustom(ctx, true)
}

// DumpCustom provides custom control over how the global scope is dumped.
func (i *runtime) DumpCustom(ctx resource.Context, recursive bool) {
	i.context.globalScope.dump(ctx, recursive)
}

// suiteContext returns the suiteContext.
func (i *runtime) suiteContext() *suiteContext {
	return i.context
}

// newRootContext creates and returns a new testContext with no parent.
func (i *runtime) newRootContext(test *testImpl, goTest *testing.T, labels label.Set) *testContext {
	return newTestContext(test, goTest, i.context, nil, labels)
}

// Close implements io.Closer
func (i *runtime) Close() error {
	return i.context.globalScope.done(i.context.settings.NoCleanup)
}
