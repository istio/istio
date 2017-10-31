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
	"reflect"
	"time"

	"istio.io/mixer/pkg/il"
)

// Extern represents an external, native function that is callable from within the interpreter,
// during program execution.
type Extern struct {
	name       string
	paramTypes []il.Type
	returnType il.Type

	v reflect.Value
}

// ExternFromFn creates a new, reflection based Extern, based on the given function. It panics if the
// function signature is incompatible to be an extern.
//
// A function can be extern under the following conditions:
// - Input parameter types are one of the supported types: string, bool, int64, float64, map[string]string.
// - The return types can be:
//   - none                                 (i.e. func (...) {...})
//   - a supported type                     (i.e. func (...) string {...})
//   - error                                (i.e. func (...) error {...})
//   - suported type and error              (i.e. func (...) string, error {...})
//
func ExternFromFn(name string, fn interface{}) Extern {
	t := reflect.TypeOf(fn)
	if t.Kind() != reflect.Func {
		panic("interpreter.ExternFromFn: not a function")
	}

	// Validate and calculate return types.
	iErr := reflect.TypeOf((*error)(nil)).Elem()
	returnType := il.Void
	switch t.NumOut() {
	case 0:

	case 1:
		if !t.Out(0).Implements(iErr) {
			returnType = ilType(t.Out(0))
		}

	case 2:
		returnType = ilType(t.Out(0))
		if !t.Out(1).Implements(iErr) {
			panic("interpreter.ExternFromFn: the second return value is not an error")
		}

	default:
		panic("interpreter.ExternFromFn: more than two return values are not allowed")
	}

	if returnType == il.Unknown {
		panic("interpreter.ExternFromFn: incompatible return type")
	}

	// Calculate parameter types.
	paramTypes := make([]il.Type, t.NumIn())
	for i := 0; i < t.NumIn(); i++ {
		pt := t.In(i)
		ilt := ilType(pt)
		if ilt == il.Unknown {
			panic("interpreter.ExternFromFn: incompatible parameter type")
		}
		paramTypes[i] = ilt
	}

	v := reflect.ValueOf(fn)

	return Extern{
		name:       name,
		paramTypes: paramTypes,
		returnType: returnType,
		v:          v,
	}
}

// ilType maps the Go reflected type to its IL counterpart.
func ilType(t reflect.Type) il.Type {
	switch t.Kind() {
	case reflect.String:
		return il.String
	case reflect.Bool:
		return il.Bool
	case reflect.Int64:
		if t.Name() == "Duration" {
			return il.Duration
		}
		return il.Integer
	case reflect.Float64:
		return il.Double
	case reflect.Map:
		if t.Key().Kind() == reflect.String || t.Elem().Kind() == reflect.String {
			return il.Interface
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return il.Interface
		}
	case reflect.Struct:
		switch t.Name() {
		case "Time":
			return il.Interface
		}
	}

	return il.Unknown
}

// invoke calls the extern function via reflection, using the interpreter's calling convention.
// The parameters are read from the stack and get converted to Go values and the extern function
// gets invoked. When the call completes, the return value, if any, gets converted back to the IL
// type and pushed on to the stack. If the extern returns an error as one of the return values,
// then the error is checked and raised in the IL if it is not nil.
//
// The function returns two uint32 values in the push order (i.e. first uint32 to be pushed on to
// the stack first).
func (e Extern) invoke(s *il.StringTable, heap []interface{}, hp *uint32, stack []uint32, sp uint32) (uint32, uint32, error) {

	// Convert the parameters on stack to reflect.Values.
	ins := make([]reflect.Value, len(e.paramTypes))

	// ap is the index to the beginning of the arguments.
	ap := sp - typesStackAllocSize(e.paramTypes)
	for i := 0; i < len(e.paramTypes); i++ {

		switch e.paramTypes[i] {
		case il.String:
			str := s.GetString(stack[ap])
			ins[i] = reflect.ValueOf(str)

		case il.Bool:
			b := stack[ap] != 0
			ins[i] = reflect.ValueOf(b)

		case il.Integer:
			iv := il.ByteCodeToInteger(stack[ap+1], stack[ap])
			ins[i] = reflect.ValueOf(iv)

		case il.Duration:
			iv := il.ByteCodeToInteger(stack[ap+1], stack[ap])
			ins[i] = reflect.ValueOf(time.Duration(iv))

		case il.Double:
			d := il.ByteCodeToDouble(stack[ap+1], stack[ap])
			ins[i] = reflect.ValueOf(d)

		case il.Interface:
			r := heap[stack[ap]]
			ins[i] = reflect.ValueOf(r)

		default:
			panic("interpreter.Extern.invoke: unrecognized parameter type")
		}

		ap += typeStackAllocSize(e.paramTypes[i])
	}

	// Perform the actual invocation through reflection.
	outs := e.v.Call(ins)

	// Convert the output values back to IL.
	var rv reflect.Value
	switch len(outs) {
	case 1:
		if e.returnType != il.Void {
			rv = outs[0]
			break
		}
		// If there is 1 return value in Go-space, but we expect the return type of the function to be
		// Void in IL, then interpret the value as error.
		if i := outs[0].Interface(); i != nil {
			return 0, 0, i.(error)
		}
	case 2:
		// If there are 2 return values in Go-space, interpret the first one as actual return value,
		// and the second one as error.
		rv = outs[0]
		if i := outs[1].Interface(); i != nil {
			return 0, 0, i.(error)
		}
	}

	// Map the return value back to IL
	switch e.returnType {
	case il.String:
		str := rv.String()
		id := s.GetID(str)
		return id, 0, nil

	case il.Bool:
		if rv.Bool() {
			return 1, 0, nil
		}
		return 0, 0, nil

	case il.Integer, il.Duration:
		i := rv.Int()
		o1, o2 := il.IntegerToByteCode(i)
		return o2, o1, nil

	case il.Double:
		d := rv.Float()
		o1, o2 := il.DoubleToByteCode(d)
		return o2, o1, nil

	case il.Interface:
		// TODO(ozben): We should single-instance the values, as they are prone to mutation.
		r := rv.Interface()
		heap[*hp] = r
		*hp++
		return *hp - 1, 0, nil

	case il.Void:
		return 0, 0, nil

	default:
		panic("interpreter.Extern.invoke: unrecognized return type")
	}
}
