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
	"reflect"
	"strings"
	"testing"
	"unsafe"

	fuzzheaders "github.com/AdaLogics/go-fuzz-headers"
	"github.com/mitchellh/copystructure"

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

type mutateCtx struct {
	t        test.Failer
	curDepth int
	maxDepth int
}

// MutateStruct modify the field value of the structure.
// It is mainly used to check the correctness of the deep copy.
func MutateStruct(t test.Failer, st any) {
	ctx := mutateCtx{t: t, curDepth: 0, maxDepth: 100}
	e := reflect.ValueOf(st).Elem()
	if err := mutateStruct(ctx, e); err != nil {
		// do not use t.Skip here, it will call runtime.Goexit()
		t.Log(err.Error())
	}
}

func mutateStruct(ctx mutateCtx, e reflect.Value) error {
	// this can happen when circular data structure
	if ctx.curDepth >= ctx.maxDepth {
		return fmt.Errorf("reach max depth and the mutation may not completed")
	}
	ctx.curDepth++
	defer func() { ctx.curDepth-- }()

	switch e.Kind() {
	case reflect.Struct:
		for i := 0; i < e.NumField(); i++ {
			var v reflect.Value
			if !e.Field(i).CanSet() {
				v = reflect.NewAt(e.Field(i).Type(), unsafe.Pointer(e.Field(i).UnsafeAddr())).Elem()
			} else {
				v = e.Field(i)
			}
			err := mutateStruct(ctx, v)
			if err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < e.Len(); i++ {
			err := mutateStruct(ctx, e.Index(i))
			if err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range e.MapKeys() {
			v := reflect.New(e.Type().Elem()).Elem()
			err := mutateStruct(ctx, v)
			if err != nil {
				return err
			}
			e.SetMapIndex(k, v)
		}
	case reflect.Ptr:
		err := mutateStruct(ctx, e.Elem())
		if err != nil {
			return err
		}
	case reflect.Bool:
		if e.CanSet() {
			old := e.Bool()
			e.SetBool(!old)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if e.CanSet() {
			old := e.Int()
			// check overflow
			if old+1 < old {
				e.SetInt(old - 1)
			} else {
				e.SetInt(old + 1)
			}
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if e.CanSet() {
			old := e.Uint()
			// check overflow
			if old+1 < old {
				e.SetUint(old - 1)
			} else {
				e.SetUint(old + 1)
			}
		}
	case reflect.Float32, reflect.Float64:
		if e.CanSet() {
			old := e.Float()
			// check overflow
			if old+1 < old {
				e.SetFloat(old - 1)
			} else {
				e.SetFloat(old + 1)
			}
		}
	case reflect.String:
		if e.CanSet() {
			// add fixed suffix
			str := e.String() + "mutated"
			e.SetString(str)
		}
	default:
		ctx.t.Logf("unimplemented type %s", e.Kind())
	}
	return nil
}

// DeepCopySlow is a general deep copy method that guarantees the correctness of deep copying,
// but may be very slow. Here, it is only used for testing.
// Note: this function not support copy struct private filed.
func DeepCopySlow[T any](v T) T {
	copied, err := copystructure.Copy(v)
	if err != nil {
		// There are 2 locations where errors are generated in copystructure.Copy:
		//  * The reflection walk over the structure fails, which should never happen
		//  * A configurable copy function returns an error. This is only used for copying times, which never returns an error.
		// Therefore, this should never happen
		panic(err)
	}
	return copied.(T)
}
