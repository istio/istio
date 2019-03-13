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

package core

import (
	"fmt"
)

const (
	// Native environment name
	Native EnvironmentName = "native"
	// Kube environment name
	Kube EnvironmentName = "kube"
)


// Environment where resources reside.
type Environment interface {
	Resource

	EnvironmentName() EnvironmentName
}

// EnvironmentName of environment
type EnvironmentName string

// String implements fmt.Stringer
func (n EnvironmentName) String() string {
	return string(n)
}

// EnvironmentNames of supported environments
func EnvironmentNames() []EnvironmentName {
	return []EnvironmentName{
		Native,
		Kube,
	}
}

// DefaultName is the name of the default environment
func DefaultEnvironmentName() EnvironmentName {
	return Native
}

// UnsupportedEnvironment generates an error indicating that the given environment is not supported.
func UnsupportedEnvironment(name EnvironmentName) error {
	return fmt.Errorf("unsupported environment: %q", name.String())
}
