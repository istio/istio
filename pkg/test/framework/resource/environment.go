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

package resource

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/components/environment"
)

// Environment is the ambient environment that the test runs in.
type Environment interface {
	Resource

	EnvironmentName() environment.Name

	// Case calls the given function if this environment has the given name.
	Case(e environment.Name, fn func())
}

// UnsupportedEnvironment generates an error indicating that the given environment is not supported.
func UnsupportedEnvironment(env Environment) error {
	return fmt.Errorf("unsupported environment: %q", string(env.EnvironmentName()))
}
