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

package internal

import (
	"istio.io/istio/pkg/test/framework/environment"
)

// EnvironmentController is the internal interface that should be implemented by Environments. This is used by the
// driver to communicate with the environments in a standard way.
type EnvironmentController interface {
	environment.Implementation

	Initialize(ctx *TestContext) error
	Configure(config string) error
	Evaluate(template string) (string, error)
	Reset() error
}
