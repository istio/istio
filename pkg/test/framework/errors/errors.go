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

package errors

import (
	"fmt"
)

// UnrecognizedEnvironment error
func UnrecognizedEnvironment(env string) error {
	return fmt.Errorf("unrecognized environment: %q", env)
}

// MissingKubeConfigForEnvironment error
func MissingKubeConfigForEnvironment(env string, envvar string) error {
	return fmt.Errorf(
		"environment %q requires kube configuration (can be specified from command-line, or with %s)",
		env, envvar)
}

// InvalidTestID error
func InvalidTestID(maxLength int) error {
	return fmt.Errorf("testID must be non-empty and cannot be longer than %d characters", maxLength)
}
