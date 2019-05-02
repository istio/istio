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

package framework

import (
	"testing"

	"istio.io/istio/pkg/test/framework/label"
)

// Run runs the given test.
func Run(t *testing.T, fn func(ctx TestContext)) {
	NewTest(t).Run(fn)
}

// NewContext creates a new test context and returns. It is upto the caller to close to context by calling
// .Done() at the end of the test run.
func NewContext(t *testing.T, labels ...label.Instance) TestContext {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt == nil {
		panic("call to scope without running the test framework")
	}

	return rt.NewTestContext(t, nil, label.NewSet(labels...))
}
