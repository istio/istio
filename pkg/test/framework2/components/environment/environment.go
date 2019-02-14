//  Copyright 2019 Istio Authors
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

package environment

import (
	"fmt"
)

// FactoryFn is used to create a new environment.
type FactoryFn func(string, Context) (Instance, error)

// Name of environment
type Name string

// String implements fmt.Stringer
func (n Name) String() string {
	return string(n)
}

// UnsupportedEnvironment generates an error indicating that the given environment is not supported.
func UnsupportedEnvironment(name Name) error {
	return fmt.Errorf("unsupported environment: %q", name.String())
}

// Instance of environment.
type Instance interface {
	Name() Name
}

// Context for environments
type Context interface {
	CreateTmpDirectory(string) (string, error)
}
