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
	"istio.io/istio/pkg/test/scopes"
)

type (
	srcSetupFn     func(t framework.TestContext, from echo.Callers) error
	svcPairSetupFn func(t framework.TestContext, from echo.Callers, to echo.Services) error
	dstSetupFn     func(t framework.TestContext, to echo.Target) error
)

// Setup runs the given function in the source deployment context.
//
// For example, given apps a, b, and c in 2 clusters,
// these tests would all run before the context is cleaned up:
//   - a/to_b/from_cluster-1
//   - a/to_b/from_cluster-2
//   - a/to_c/from_cluster-1
//   - a/to_c/from_cluster-2
//   - cleanup...
//   - b/to_a/from_cluster-1
//     ...
func (t *T) Setup(setupFn srcSetupFn) *T {
	t.sourceDeploymentSetup = append(t.sourceDeploymentSetup, setupFn)
	return t
}

func (t *T) setup(ctx framework.TestContext, from echo.Callers) {
	if !t.hasSourceSetup() {
		ctx.SkipDumping()
		scopes.Framework.Debugf("No echotest setup; skipping test dump at this scope.")
	}
	for _, setupFn := range t.sourceDeploymentSetup {
		if err := setupFn(ctx, from); err != nil {
			ctx.Fatal(err)
		}
	}
}

func (t *T) hasSourceSetup() bool {
	return len(t.sourceDeploymentSetup) > 0
}

// SetupForPair runs the given function for every source instance in every cluster in combination with every
// destination service.
//
// Example of how long this setup lasts before the given context is cleaned up:
//   - a/to_b/from_cluster-1
//   - a/to_b/from_cluster-2
//   - cleanup...
//   - a/to_b/from_cluster-2
//   - ...
func (t *T) SetupForPair(setupFn func(ctx framework.TestContext, from echo.Callers, dsts echo.Instances) error) *T {
	return t.SetupForServicePair(func(ctx framework.TestContext, from echo.Callers, dsts echo.Services) error {
		return setupFn(ctx, from, dsts.Instances())
	})
}

// SetupForServicePair works similarly to SetupForPair, but the setup function accepts echo.Services, which
// contains instances for multiple services and should be used in combination with RunForN.
// The length of dsts services will alyas be N.
func (t *T) SetupForServicePair(setupFn svcPairSetupFn) *T {
	t.deploymentPairSetup = append(t.deploymentPairSetup, setupFn)
	return t
}

// SetupForDestination is run each time the destination Service (but not destination cluser) changes.
func (t *T) SetupForDestination(setupFn dstSetupFn) *T {
	t.destinationDeploymentSetup = append(t.destinationDeploymentSetup, setupFn)
	return t
}

func (t *T) hasDestinationSetup() bool {
	return len(t.deploymentPairSetup)+len(t.destinationDeploymentSetup) > 0
}

func (t *T) setupPair(ctx framework.TestContext, from echo.Callers, dsts echo.Services) {
	if !t.hasDestinationSetup() {
		ctx.SkipDumping()
		scopes.Framework.Debugf("No echotest setup; skipping test dump at this scope.")
	}
	for _, setupFn := range t.deploymentPairSetup {
		if err := setupFn(ctx, from, dsts); err != nil {
			ctx.Fatal(err)
		}
	}
	for _, setupFn := range t.destinationDeploymentSetup {
		if err := setupFn(ctx, dsts.Instances()); err != nil {
			ctx.Fatal(err)
		}
	}
}
