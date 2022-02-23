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
)

type (
	srcSetupFn     func(ctx framework.TestContext, src echo.Callers) error
	svcPairSetupFn func(ctx framework.TestContext, src echo.Callers, dsts echo.Services) error
	dstSetupFn     func(ctx framework.TestContext, dsts echo.Instances) error
)

// Setup runs the given function in the source deployment context.
//
// For example, given apps a, b, and c in 2 clusters,
// these tests would all run before the the context is cleaned up:
//     a/to_b/from_cluster-1
//     a/to_b/from_cluster-2
//     a/to_c/from_cluster-1
//     a/to_c/from_cluster-2
//     cleanup...
//     b/to_a/from_cluster-1
//     ...
func (t *T) Setup(setupFn srcSetupFn) *T {
	t.sourceDeploymentSetup = append(t.sourceDeploymentSetup, setupFn)
	return t
}

func (t *T) setup(ctx framework.TestContext, srcInstances echo.Callers) {
	if !t.hasSourceSetup() {
		ctx.SkipDumping()
		ctx.Logf("No echotest setup; skipping test dump at this scope.")
	}
	for _, setupFn := range t.sourceDeploymentSetup {
		if err := setupFn(ctx, srcInstances); err != nil {
			ctx.Fatal(err)
		}
	}
}

func (t *T) setupDest(ctx framework.TestContext, dstInstances echo.Instances) {
	if !t.hasDestinationSetup() {
		ctx.SkipDumping()
		ctx.Logf("No echotest setup; skipping test dump at this scope.")
	}
	for _, setupFn := range t.destinationDeploymentSetup {
		if err := setupFn(ctx, dstInstances); err != nil {
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
//     a/to_b/from_cluster-1
//     a/to_b/from_cluster-2
//     cleanup...
//     a/to_b/from_cluster-2
//     ...
func (t *T) SetupForPair(setupFn func(ctx framework.TestContext, src echo.Callers, dsts echo.Instances) error) *T {
	return t.SetupForServicePair(func(ctx framework.TestContext, src echo.Callers, dsts echo.Services) error {
		return setupFn(ctx, src, dsts.Instances())
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

func (t *T) setupPair(ctx framework.TestContext, src echo.Callers, dsts echo.Services) {
	if !t.hasDestinationSetup() {
		ctx.SkipDumping()
		ctx.Logf("No echotest setup; skipping test dump at this scope.")
	}
	for _, setupFn := range t.deploymentPairSetup {
		if err := setupFn(ctx, src, dsts); err != nil {
			ctx.Fatal(err)
		}
	}
	for _, setupFn := range t.destinationDeploymentSetup {
		if err := setupFn(ctx, dsts.Instances()); err != nil {
			ctx.Fatal(err)
		}
	}
}

func (t *T) setupDstPair(ctx framework.TestContext, src echo.Callers, dsts echo.Services) {
	if !t.hasSourceSetup() {
		ctx.SkipDumping()
		ctx.Logf("No echotest setup; skipping test dump at this scope.")
	}
	for _, setupFn := range t.deploymentPairSetup {
		if err := setupFn(ctx, src, dsts); err != nil {
			ctx.Fatal(err)
		}
	}
	for _, setupFn := range t.sourceDeploymentSetup {
		if err := setupFn(ctx, src); err != nil {
			ctx.Fatal(err)
		}
	}
}

func (t *T) GetWorkloads() (echo.Services, echo.Services) {
	return t.destinations.Services(), t.sources.Services()
}
