// Copyright Istio Authors
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

package echotest

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/match"
)

// T enumerates subtests given a set of workloads as echo.Instances.
type T struct {
	// rootCtx is the top level test context to generate subtests from and should only be referenced from RunX methods.
	rootCtx framework.TestContext

	sources      echo.Instances
	destinations echo.Instances

	destinationFilters []CombinationFilter

	sourceDeploymentSetup      []srcSetupFn
	deploymentPairSetup        []svcPairSetupFn
	destinationDeploymentSetup []dstSetupFn

	cfg *config.Builder
}

// New creates a *T using the given applications as sources and destinations for each subtest.
func New(ctx framework.TestContext, instances echo.Instances) *T {
	s, d := make(echo.Instances, len(instances)), make(echo.Instances, len(instances))
	copy(s, instances)
	copy(d, instances)
	t := &T{
		rootCtx:      ctx,
		cfg:          config.New(ctx),
		sources:      s,
		destinations: d,
	}
	if ctx.Settings().Skip(echo.VM) {
		t = t.FromMatch(match.NotVM).ToMatch(match.NotVM)
	}
	return t
}
