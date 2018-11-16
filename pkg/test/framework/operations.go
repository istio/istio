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
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/runtime"
	"testing"
)

var r = runtime.New()

// Run is a helper for executing test main with appropriate resource allocation/doCleanup steps.
// It allows us to do post-run doCleanup, and flag parsing.
func Run(testID string, m *testing.M) {
	r.Run(testID, m)
}

// GetContext resets and returns the environment. Should be called exactly once per test.
func GetContext(t testing.TB) context.Instance {
	return r.GetContext(t)
}
