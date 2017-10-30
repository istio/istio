// Copyright 2017 Istio Authors
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

package interpreter

import (
	"testing"

	"istio.io/mixer/pkg/il"
)

func TestExternFromFn_NotAFunction(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	ExternFromFn("foo", 23)
}

func TestExternFromFn_MoreThanTwoOutParameteters(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	ExternFromFn("foo", func() (int64, int64, int64) { return 0, 0, 0 })
}

func TestExternFromFn_TwoOutParametetersWithoutError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	ExternFromFn("foo", func() (int64, int64) { return 0, 0 })
}

func TestExternFromFn_IncompatibleReturnType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	ExternFromFn("foo", func() int { return 0 })
}

func TestExternFromFn_IncompatibleParameterType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	ExternFromFn("foo", func(int) {})
}

func TestExternFromFn_UnrecognizedParamTypeDuringInvoke(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	e := ExternFromFn("foo", func(int64) {})
	e.paramTypes[0] = il.Void

	p := il.NewProgram()
	heap := make([]interface{}, heapSize)
	stack := make([]uint32, opStackSize)
	sp := uint32(2)
	hp := uint32(0)
	_, _, _ = e.invoke(p.Strings(), heap, &hp, stack, sp)
}

func TestExternFromFn_UnrecognizedReturnType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	e := ExternFromFn("foo", func() int64 { return 0 })
	e.returnType = il.Unknown

	p := il.NewProgram()
	heap := make([]interface{}, heapSize)
	stack := make([]uint32, opStackSize)
	sp := uint32(0)
	hp := uint32(0)
	_, _, _ = e.invoke(p.Strings(), heap, &hp, stack, sp)
}
