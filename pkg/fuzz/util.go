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

package fuzz

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	fuzzheaders "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pkg/test"
)

const panicPrefix = "go-fuzz-skip: "

// Helper is a helper struct for fuzzing
type Helper struct {
	cf *fuzzheaders.ConsumeFuzzer
	t  test.Failer
}

type Validator interface {
	// FuzzValidate returns true if the current struct is valid for fuzzing.
	FuzzValidate() bool
}

// Fuzz is a wrapper around:
//
//	 fuzz.BaseCases(f)
//		f.Fuzz(func(...) {
//		   defer fuzz.Finalizer()
//		}
//
// To avoid needing to call BaseCases and Finalize everywhere.
func Fuzz(f test.Fuzzer, ff func(fg Helper)) {
	BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		defer Finalize()
		fg := New(t, data)
		ff(fg)
	})
}

// Finalize works around an issue in the oss-fuzz logic that doesn't allow using Skip()
// Instead, we send a panic which we handle and treat as skip.
// https://github.com/AdamKorcz/go-118-fuzz-build/issues/6
func Finalize() {
	if r := recover(); r != nil {
		if s, ok := r.(string); ok {
			if strings.HasPrefix(s, panicPrefix) {
				return
			}
		}
		panic(r)
	}
}

// New creates a new fuzz.Helper, capable of generating more complex types
func New(t test.Failer, data []byte) Helper {
	return Helper{cf: fuzzheaders.NewConsumer(data), t: t}
}

// Struct generates a Struct. Validation patterns can be passed in - if any return false, the fuzz case is skipped.
// Additionally, if the T implements Validator, it will implicitly be used.
func Struct[T any](h Helper, validators ...func(T) bool) T {
	d := new(T)
	if err := h.cf.GenerateStruct(d); err != nil {
		h.t.Skip(err.Error())
	}
	r := *d
	validate(h, validators, r)
	return r
}

// Slice generates a slice of Structs
func Slice[T any](h Helper, count int, validators ...func(T) bool) []T {
	if count < 0 {
		// Make it easier to just pass fuzzer generated counts, typically with %max applied
		count *= -1
	}
	res := make([]T, 0, count)
	for i := 0; i < count; i++ {
		d := new(T)
		if err := h.cf.GenerateStruct(d); err != nil {
			h.t.Skip(err.Error())
		}
		r := *d
		validate(h, validators, r)
		res = append(res, r)
	}
	return res
}

func validate[T any](h Helper, validators []func(T) bool, r T) {
	if fz, ok := any(r).(Validator); ok {
		if !fz.FuzzValidate() {
			h.t.Skip("struct didn't pass validator")
		}
	}
	for i, v := range validators {
		if !v(r) {
			h.t.Skip(fmt.Sprintf("struct didn't pass validator %d", i))
		}
	}
}

// BaseCases inserts a few trivial test cases to do a very brief sanity check of a test that relies on []byte inputs
func BaseCases(f test.Fuzzer) {
	for _, c := range [][]byte{
		{},
		[]byte("."),
		bytes.Repeat([]byte("."), 1000),
	} {
		f.Add(c)
	}
}

// T Returns the underlying test.Failer. Should be avoided where possible; in oss-fuzz many functions do not work.
func (h Helper) T() test.Failer {
	return h.t
}
