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

package template

import "fmt"

// ErrorPath represents an error that occurred within a complicated object hierarchy. It allows keeping track of
// the path to the error location during unwinding, while keeping the amount of garbage generated to a minimum.
type ErrorPath struct {
	path  string
	cause error
}

// NewErrorPath gets called, during the unwinding of a generated call in a complicated object hierarchy, to start
// tracking of the location that the error has occurred.
func NewErrorPath(path string, cause error) ErrorPath {
	return ErrorPath{
		path:  path,
		cause: cause,
	}
}

// WithPrefix adds the given prefix to the error path. Typically, this is called by an intermediate step during unwind
// to attach its own location information, before returning it up to the caller.
func (e ErrorPath) WithPrefix(prefix string) ErrorPath {
	return ErrorPath{
		path:  prefix + "." + e.path,
		cause: e.cause,
	}
}

// IsNil returns if the cause in ErrorPath is nil
func (e ErrorPath) IsNil() bool {
	return e.cause == nil
}

// AsEvaluationError creates an actual error to be used in the evaluation path.
func (e ErrorPath) AsEvaluationError(instance string) error {
	return fmt.Errorf("evaluation failed at [%s]'%s': '%v'", instance, e.path, e.cause)
}

// AsCompilationError creates an actual error to be used in the compilation path.
func (e ErrorPath) AsCompilationError(instance string) error {
	return fmt.Errorf("expression compilation failed at [%s]'%s': '%v'", instance, e.path, e.cause)
}
