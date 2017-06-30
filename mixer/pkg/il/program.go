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

// Package il implements the intermediate-language for the config-language interpreter of Mixer.
// The IL is designed as a 32-bit instruction set. The opcode/argument size of the IL is 32-bits
// and both are modeled as uint32s. Different instructions can have different number of arguments,
// but a given instruction will always have a well-defined number of arguments. Each argument
// can be 1 or 2 uint32s, depending on type.
//
// The IL intrinsically supports the following basic primitive types:
//
//  - String: represents a string value and maps to the native string Go type. Internally, the
//  strings are interned in a StringTable, thus allowing extremely fast equality comparisons. The
//  id of the string in the StringTable is encoded in the ByteCode, when stored, or during execution.
//  - Bool: represents a boolean value and maps to the native bool Go type. The boolean values of
//  false and true are represented as uint32(0) and uint32(1), respectively. The VM will consider
//  any non-zero value as true, if it encounters during execution.
//  - Integer: represents a signed integer value and maps to the native int64 Go type. The integer
//  is stored in 2 uint32s internally.
//  - Double: represents a signed float value and maps to the native float64 Go type. The double
//  is stored in 2 uint32s internally.
//  - Record represents a mapping of string-> string and maps to the map[string]string Go Type.
// There is no constant representation of Record in il form.
//  - Void represents no-value. This is typically used in the return type of a function, to indicate
// that the function does not return a result.
//
// In addition to these types, the VM inherently supports errors, based on Go errors. It is possible
// to raise these errors from within the IL. However, it is not possible to trap or return these as
// values. Once an error is raised, whether as a program instruction, or due to an invalid evaluation
// step, it bubbles all the way out of the evaluation context.
//
// The execution model is based on an operand stack and registers. Though most operations work on an
// operand stack to perform calculations, it is possible to transfer data from registers to stack
// and vice-versa.
//
// The IL supports a stack-based calling-convention. All the parameters to a function need to be
// pushed onto the stack before the CALL instruction is invoked. Similarly, the return values need
// to be pushed onto the stack when RET is called.
//
// Typically, an IL based program is created by a compiler by initializing a Program type and
// by adding Functions to it, whose bodies are built using the Builder type. Once built, programs
// can be serialized/deserialized into a textual form for ease of use, using Write* and Read* method
// in this package.
//
package il

import (
	"fmt"
)

const (
	// programCodeSize is the default size of a program.
	defaultProgramCodeSize int = 256
)

// Program is a self-contained IL based set of functions that can be executed by an interpreter.
type Program struct {
	// Strings is the collection of strings that is used by the program.
	strings *StringTable

	// Functions is the collection of functions that are contained in the program.
	Functions *FunctionTable

	// Code is the actual byte-code based body of all the functions in the program.
	code []uint32
}

// NewProgram creates and returns a new, empty program.
func NewProgram() *Program {
	strings := newStringTable()
	p := &Program{
		strings:   strings,
		Functions: newFunctionTable(strings),
		code:      make([]uint32, 0, defaultProgramCodeSize),
	}
	p.code = append(p.code, uint32(Halt))

	return p
}

// AddExternDef adds a new external/native function to the program. The actual bodies of functions
// need to be supplied to the interpreter separately.
func (p *Program) AddExternDef(name string, parameters []Type, returnType Type) {
	f := Function{
		ID:         p.strings.GetID(name),
		Length:     0,
		Address:    0,
		Parameters: parameters,
		ReturnType: returnType,
	}
	p.Functions.add(&f)
}

// AddFunction adds a new IL based function to the program.
func (p *Program) AddFunction(name string, parameters []Type, returnType Type, body []uint32) error {
	p.code = append(p.code, uint32(Halt)) // Leave a single instruction gap between function bodies.

	start := uint32(len(p.code))
	l := uint32(len(body))

	// Rewrite jump addresses from local to global when transcribing method body.
	var i uint32
	for i = 0; i < l; {
		op := Opcode(body[i])
		info := opCodeInfos[op]

		if i+op.Size() > l {
			return fmt.Errorf("opcode requires more arguments than are present in the body: op: %v, loc: %d", op, i)
		}
		p.code = append(p.code, body[i])
		i++
		for _, a := range info.args {
			for j := 0; j < int(a.Size()); j++ {
				p.code = append(p.code, body[i])
				if a == OpcodeArgAddress {
					p.code[len(p.code)-1] += start
				}
				i++
			}
		}
	}

	f := &Function{
		ID:         p.strings.GetID(name),
		Length:     l,
		Address:    start,
		Parameters: parameters,
		ReturnType: returnType,
	}
	p.Functions.add(f)

	return nil
}

// Strings returns the strings table of this program.
func (p *Program) Strings() *StringTable {
	return p.strings
}

// ByteCode returns a copy of the byte-code of this program.
func (p *Program) ByteCode() []uint32 {
	result := make([]uint32, len(p.code))
	copy(result, p.code)
	return result
}
