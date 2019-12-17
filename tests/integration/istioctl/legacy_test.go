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

package istioctl

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/label"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("nop-test", m).
		Label(label.CustomSetup).
		Run()
}

// Legacy CI targets try to call this folder and fail if there are no tests. Until that is cleaned up,
// Have a nop test.
func TestNothing(t *testing.T) {
	// You can specify additional constraints using the more verbose form
	framework.NewTest(t).
		Label(label.Postsubmit).
		RequiresEnvironment(environment.Native).
		Run(func(ctx framework.TestContext) {})
}
