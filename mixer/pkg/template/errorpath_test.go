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

import (
	"fmt"
	"testing"
)

func TestNewErrorPath(t *testing.T) {
	e := NewErrorPath("foo", fmt.Errorf("submarine is not yellow"))

	if e.path != "foo" {
		t.Fail()
	}

	if e.cause == nil || e.cause.Error() != "submarine is not yellow" {
		t.Fail()
	}
}

func TestErrorPath_WithPrefix(t *testing.T) {
	e := NewErrorPath("foo", fmt.Errorf("submarine is not yellow"))
	e = e.WithPrefix("bar")

	if e.path != "bar.foo" {
		t.Fail()
	}

	if e.cause == nil || e.cause.Error() != "submarine is not yellow" {
		t.Fail()
	}
}

func TestErrorPath_IsNil(t *testing.T) {
	e := ErrorPath{}
	if !e.IsNil() {
		t.Fail()
	}

	e = NewErrorPath("foo", fmt.Errorf("submarine is not yellow"))
	if e.IsNil() {
		t.Fail()
	}
}

func TestErrorPath_AsEvaluationError(t *testing.T) {
	e := NewErrorPath("foo", fmt.Errorf("submarine is not yellow"))
	err := e.AsEvaluationError("instance1")
	if err == nil {
		t.Fail()
	}

	if err.Error() != "evaluation failed at [instance1]'foo': 'submarine is not yellow'" {
		t.Fail()
	}
}

func TestErrorPath_AsCompilationError(t *testing.T) {
	e := NewErrorPath("foo", fmt.Errorf("submarine is not yellow"))
	err := e.AsCompilationError("instance1")
	if err == nil {
		t.Fail()
	}

	if err.Error() != "expression compilation failed at [instance1]'foo': 'submarine is not yellow'" {
		t.Fail()
	}
}
