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

package component

import (
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"testing"
)

// ResolutionError is an error related to resolving component dependencies.
type ResolutionError error

// StartError is an error encountered while attempting to start a component.
type StartError error

// Resolver for component requirements. It controls creation of the dependency tree for each required component.
type Resolver interface {
	// Require the given components to be available with the given lifecycle scope. The components may be specified
	// via ID or specifically by descriptor. If a component requires others, each of its required components are
	// implicitly required with the same scope. If a component already exists with the requested scope (or higher),
	// the existing component is used. If an error was encountered related to resolving the dependency chain of a
	// required component, the ResolutionError will be returned. If an error occurred while starting one of the
	// components, a StartError is returned.
	Require(scope lifecycle.Scope, reqs ...Requirement) (ResolutionError, StartError)

	// RequireOrFail calls Require and fails the test if any error occurs.
	RequireOrFail(t testing.TB, scope lifecycle.Scope, reqs ...Requirement)

	// RequireOrSkip calls Require and skips the test if a ResolutionError occurs. If a StartError occurs, however,
	// the test still fails.
	RequireOrSkip(t testing.TB, scope lifecycle.Scope, reqs ...Requirement)
}
