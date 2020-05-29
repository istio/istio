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

package il

// Opcode is the type for the opcodes in the il.
type Opcode uint32

// OpcodeArg represents the type of the arguments that opcodes have.
type OpcodeArg int

// opcodeInfo holds metadata about an opcode.
type opcodeInfo struct {
	// The human-readable name of the opcode.
	name string

	// The string keyword matching this opcode.
	keyword string

	// The set of arguments that is expected by this opcode.
	args []OpcodeArg
}

const (
	// Halt stops the VM with an error.
	Halt Opcode = 0

	// Nop does nothing.
	Nop Opcode = 1

	// Err raises an error.
	Err Opcode = 2

	// Errz pops a boolean value from stack and check its value. if the value is false, then it
	// raises an error.
	Errz Opcode = 3

	// Errnz pops a boolean value from stack and check its value. if the value is true, then it
	// raises an error.
	Errnz Opcode = 4

	// PopS pops a string from the stack.
	PopS Opcode = 10

	// PopB pops a bool from the stack.
	PopB Opcode = 11

	// PopI pops an integer from the stack.
	PopI Opcode = 12

	// PopD pops a double from the stack.
	PopD Opcode = 13

	// DupS pops a string value from the stack and pushes it back into the stack twice.
	DupS Opcode = 14

	// DupB pops a boolean value from the stack and pushes it back into the stack twice.
	DupB Opcode = 15

	// DupI pops an integer value from the stack and pushes it back into the stack twice.
	DupI Opcode = 16

	// DupD pops a double value from the stack and pushes it back into the stack twice.
	DupD Opcode = 17

	// RLoadS pops a string from the stack and stores in the target register.
	RLoadS Opcode = 20

	// RLoadB pops a bool from the stack and stores in the target register.
	RLoadB Opcode = 21

	// RLoadI pops an integer from the stack and stores in the target register.
	RLoadI Opcode = 22

	// RLoadD pops a double from the stack and stores in the target register.
	RLoadD Opcode = 23

	// ALoadS loads its string argument into the target register.
	ALoadS Opcode = 30

	// ALoadB loads its boolean argument into the target register.
	ALoadB Opcode = 31

	// ALoadI loads its integer argument into the target register.
	ALoadI Opcode = 32

	// ALoadD loads its double argument into the target register.
	ALoadD Opcode = 33

	// APushS pushes its string argument to the stack.
	APushS Opcode = 40

	// APushB pushes its boolean argument to the stack.
	APushB Opcode = 41

	// APushI pushes its integer argument to the stack.
	APushI Opcode = 42

	// APushD pushes its double argument to the stack.
	APushD Opcode = 43

	// RPushS reads a string value from the target register and pushes it into stack.
	RPushS Opcode = 50

	// RPushB reads a boolean value from the target register and pushes it into stack.
	RPushB Opcode = 51

	// RPushI reads an integer value from the target register and pushes it into stack.
	RPushI Opcode = 52

	// RPushD reads a double value from the target register and pushes it into stack.
	RPushD Opcode = 53

	// EqS pops two string values from the stack and compare for equality. If the strings are
	// equal then it pushes 1 into the stack, otherwise it pushes 0.
	EqS Opcode = 60

	// EqB pops two bool values from the stack and compare for equality. If equal, then it
	// pushes 1 into the stack, otherwise it pushes 0.
	EqB Opcode = 61

	// EqI pops two integer values from the stack and compare for equality. If equal, then it
	// pushes 1 into the stack, otherwise it pushes 0.
	EqI Opcode = 62

	// EqD pops two double values from the stack and compare for equality. If equal, then it
	// pushes 1 into the stack, otherwise it pushes 0.
	EqD Opcode = 63

	// AEqS pops a string value from the stack and compare it against its argument for equality.
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqS Opcode = 70

	// AEqB pops a bool value from the stack and compare it against its argument for equality.
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqB Opcode = 71

	// AEqI pops an integer value from the stack and compare it against its argument for equality
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqI Opcode = 72

	// AEqD pops a double value from the stack and compare it against its argument for equality
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqD Opcode = 73

	// Xor pops two boolean values from the stack, performs logical exclusive-or, then pushes the
	// result back into stack.
	Xor Opcode = 80

	// And pops two boolean values from the stack, performs logical and, then pushes the
	// result back into stack.
	And Opcode = 81

	// Or pops two boolean values from the stack, performs logical or, then pushes the
	// result back into stack.
	Or Opcode = 82

	// AXor pops a boolean value from the stack and performs logical exclusive-or with its
	// argument, then pushes the result back into stack.
	AXor Opcode = 83

	// AAnd pops a boolean value from the stack and performs logical and with its
	// argument, then pushes the result back into stack.
	AAnd Opcode = 84

	// AOr pops a boolean value from the stack and performs logical or with its
	// argument, then pushes the result back into stack.
	AOr Opcode = 85

	// Not pops a boolean value from the stack and performs logical not,
	// then pushes the result back into stack.
	Not Opcode = 86

	// ResolveS lookups up a string attribute value in the bag, with the given name.
	// If successful, pushes the resolved string into stack, otherwise raises error.
	ResolveS Opcode = 90

	// ResolveB lookups up a bool attribute value in the bag, with the given name.
	// If successful, pushes the resolved bool into stack, otherwise raises error.
	ResolveB Opcode = 91

	// ResolveI lookups up an integer attribute value in the bag, with the given name.
	// If successful, pushes the resolved integer into stack, otherwise raises error.
	ResolveI Opcode = 92

	// ResolveD lookups up a double attribute value in the bag, with the given name.
	// If successful, pushes the resolved double into stack, otherwise raises error.
	ResolveD Opcode = 93

	// ResolveF lookups up a interface{} attribute value in the bag, with the given name.
	// If successful, pushes the resolved interface{} into stack, otherwise raises error.
	ResolveF Opcode = 94

	// TResolveS lookups up a string attribute value in the bag, with the given name.
	// If successful, pushes the resolved string value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveS Opcode = 100

	// TResolveB lookups up a  bool attribute value in the bag, with the given name.
	// If successful, pushes the resolved bool value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveB Opcode = 101

	// TResolveI lookups up an integer attribute value in the bag, with the given name.
	// If successful, pushes the resolved integer value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveI Opcode = 102

	// TResolveD lookups up a double attribute value in the bag, with the given name.
	// If successful, pushes the resolved double value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveD Opcode = 103

	// TResolveF lookups up a interface{} attribute value in the bag, with the given name.
	// If successful, pushes the resolved interface{} value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveF Opcode = 104

	// AddI pops two integer values from the stack, adds their value and pushes the result
	// back into stack. The operation follows Go's integer addition semantics.
	AddI Opcode = 110

	// AddD pops two double values from the stack, adds their value and pushes the result
	// back into stack. The operation follows Go's float addition semantics.
	AddD Opcode = 111

	// SubI pops two integer values from the stack, and subtracts the second popped value
	// from the first one, then pushes the result back into stack.
	// The operation follows Go's integer subtraction semantics.
	SubI Opcode = 112

	// SubD pops two double values from the stack, and subtracts the second popped value
	// from the first one, then pushes the result back into stack.
	// The operation follows Go's float subtraction semantics.
	SubD Opcode = 113

	// AAddI pops an integer value from the stack, adds the popped value and its argument,
	// and pushes the result back into stack. The operation follows Go's integer addition
	// semantics.
	AAddI Opcode = 114

	// AAddD pops a double value from the stack, adds the popped value and its argument,
	// and pushes the result back into stack. The operation follows Go's double addition
	// semantics.
	AAddD Opcode = 115

	// ASubI pops an integer value from the stack, subtracts its argument from the popped value,
	// then pushes the result back into stack.  The operation follows Go's integer subtraction
	// semantics.
	ASubI Opcode = 116

	// ASubD pops a double value from the stack, subtracts its argument from the popped value,
	// then pushes the result back into stack.  The operation follows Go's double subtraction
	// semantics.
	ASubD Opcode = 117

	// Jmp jumps to the given instruction address.
	Jmp Opcode = 200

	// Jz pops a bool value from the stack. If the value is zero, then jumps to the given
	// instruction address.
	Jz Opcode = 201

	// Jnz pops a bool value from the stack. If the value is not 0, then jumps to the given
	// instruction address.
	Jnz Opcode = 202

	// Call invokes the target function.
	Call Opcode = 203

	// Ret returns from the current function.
	Ret Opcode = 204

	// Lookup pops a string, then a stringmap from the stack and perform a lookup on the stringmap
	// using the string as the name. If a value is found, then the value is pushed into the
	// stack.  Otherwise raises an error.
	Lookup Opcode = 210

	// TLookup pops a string, then a stringmap from the stack and perform a lookup on the stringmap
	// using the string as the name. If a value is found, then the value is pushed into the
	// stack, then 1.  Otherwise 0 is pushed to into the stack.
	TLookup Opcode = 211

	// ALookup pops a stringmap from the stack and perform a lookup on the stringmap using the string
	// parameter as the name. If a value is found, then the value is pushed into the stack
	// Otherwise raises an error.
	ALookup Opcode = 212

	// NLookup pops a string, then a stringmap from the stack and perform a lookup on the stringmap
	// using the string as the name. If a value is found, then the value is pushed into the
	// stack.  Otherwise empty string is pushed onto the stack.
	NLookup Opcode = 213

	// ANLookup pops a stringmap from the stack and perform a lookup on the stringmap using the string
	// parameter as the name. If a value is found, then the value is pushed into the stack
	// Otherwise empty string is pushed onto the stack.
	ANLookup Opcode = 214

	// AddS pops two string values from the stack, adds their value and pushes the result
	// back into stack. The operation follows Go's string concatenation semantics.
	AddS Opcode = 215

	// SizeS pops a string value from the stack, and pushes its length back into stack.
	SizeS Opcode = 216

	// LtS pops two string values from the stack and compare for order.
	// If less then it pushes 1 into the stack, otherwise it pushes 0.
	LtS Opcode = 217

	// LtI pops two integer values from the stack and compare for order.
	// If less then it pushes 1 into the stack, otherwise it pushes 0.
	LtI Opcode = 218

	// LtD pops two float values from the stack and compare for order.
	// If less then it pushes 1 into the stack, otherwise it pushes 0.
	LtD Opcode = 219

	// ALtS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is less, then it pushes 1 into the stack, otherwise it pushes 0.
	ALtS Opcode = 220

	// ALtI pops an integer value from the stack and compare it against its argument for order.
	// If the value on the stack is less, then it pushes 1 into the stack, otherwise it pushes 0.
	ALtI Opcode = 221

	// ALtD pops a float value from the stack and compare it against its argument for order.
	// If the value on the stack is less, then it pushes 1 into the stack, otherwise it pushes 0.
	ALtD Opcode = 222

	// LeS pops two string values from the stack and compare for order.
	// If less or equals then it pushes 1 into the stack, otherwise it pushes 0.
	LeS Opcode = 223

	// LeI pops two integer values from the stack and compare for order.
	// If less or equals then it pushes 1 into the stack, otherwise it pushes 0.
	LeI Opcode = 224

	// LeD pops two float values from the stack and compare for order.
	// If less or equals then it pushes 1 into the stack, otherwise it pushes 0.
	LeD Opcode = 225

	// ALeS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is less or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	ALeS Opcode = 226

	// ALeI pops an integer value from the stack and compare it against its argument for order.
	// If the value on the stack is less or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	ALeI Opcode = 227

	// ALeD pops a float value from the stack and compare it against its argument for order.
	// If the value on the stack is less or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	ALeD Opcode = 228

	// GtS pops two string values from the stack and compare for order.
	// If greater then it pushes 1 into the stack, otherwise it pushes 0.
	GtS Opcode = 229

	// GtI pops two integer values from the stack and compare for order.
	// If greater then it pushes 1 into the stack, otherwise it pushes 0.
	GtI Opcode = 230

	// GtD pops two float values from the stack and compare for order.
	// If greater then it pushes 1 into the stack, otherwise it pushes 0.
	GtD Opcode = 231

	// AGtS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is greater, then it pushes 1 into the stack, otherwise it pushes 0.
	AGtS Opcode = 232

	// AGtI pops an integer value from the stack and compare it against its argument for order.
	// If the value on the stack is greater, then it pushes 1 into the stack, otherwise it pushes 0.
	AGtI Opcode = 233

	// AGtD pops a float value from the stack and compare it against its argument for order.
	// If the value on the stack is greater, then it pushes 1 into the stack, otherwise it pushes 0.
	AGtD Opcode = 234

	// GeS pops two string values from the stack and compare for order.
	// If greater or equal then it pushes 1 into the stack, otherwise it pushes 0.
	GeS Opcode = 235

	// GeI pops two integer values from the stack and compare for order.
	// If greater or equal then it pushes 1 into the stack, otherwise it pushes 0.
	GeI Opcode = 236

	// GeD pops two float values from the stack and compare for order.
	// If greater or equal then it pushes 1 into the stack, otherwise it pushes 0.
	GeD Opcode = 237

	// AGeS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is greater or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AGeS Opcode = 238

	// AGeI pops an integer value from the stack and compare it against its argument for equality.
	// If greater or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AGeI Opcode = 239

	// AGeD pops a float value from the stack and compare it against its argument for equality.
	// If greater or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AGeD Opcode = 240
)

const (
	// OpcodeArgRegister represents an argument that references a register.
	OpcodeArgRegister OpcodeArg = 0

	// OpcodeArgString represents an argument that is a string.
	OpcodeArgString OpcodeArg = 1

	// OpcodeArgInt represents an argument that is an integer.
	OpcodeArgInt OpcodeArg = 2

	// OpcodeArgDouble represents an argument that is a double.
	OpcodeArgDouble OpcodeArg = 3

	// OpcodeArgBool represents an argument that is a boolean.
	OpcodeArgBool OpcodeArg = 4

	// OpcodeArgFunction represents an argument that is a function.
	OpcodeArgFunction OpcodeArg = 5

	// OpcodeArgAddress represents an argument that is an address.
	OpcodeArgAddress OpcodeArg = 6
)

var argSizes = map[OpcodeArg]uint32{
	OpcodeArgRegister: 1,
	OpcodeArgString:   1,
	OpcodeArgBool:     1,
	OpcodeArgFunction: 1,
	OpcodeArgAddress:  1,
	OpcodeArgDouble:   2,
	OpcodeArgInt:      2,
}

var opCodeInfos = map[Opcode]opcodeInfo{

	// Halt stops the VM with an error.
	Halt: {name: "Halt", keyword: "halt"},

	// Nop does nothing.
	Nop: {name: "Nop", keyword: "nop"},

	// Err raises an error.
	Err: {name: "Err", keyword: "err", args: []OpcodeArg{
		// Text of the error.
		OpcodeArgString,
	}},

	// Errz pops a boolean value from stack and check its value. if the value is false, then it
	// raises an error.
	Errz: {name: "Errz", keyword: "errz", args: []OpcodeArg{
		// Text of the error message.
		OpcodeArgString,
	}},

	// Errnz pops a boolean value from stack and check its value. if the value is true, then it
	// raises an error.
	Errnz: {name: "Errnz", keyword: "errnz", args: []OpcodeArg{
		// Text of the error message.
		OpcodeArgString,
	}},

	// PopS pops a string from the stack.
	PopS: {name: "PopS", keyword: "pop_s"},

	// PopB pops a bool from the stack.
	PopB: {name: "PopB", keyword: "pop_b"},

	// PopI pops an integer from the stack.
	PopI: {name: "PopI", keyword: "pop_i"},

	// PopD pops a double from the stack.
	PopD: {name: "PopD", keyword: "pop_d"},

	// DupS pops a string value from the stack and pushes it back into the stack twice.
	DupS: {name: "DupS", keyword: "dup_s"},

	// DupB pops a boolean value from the stack and pushes it back into the stack twice.
	DupB: {name: "DupB", keyword: "dup_b"},

	// DupI pops an integer value from the stack and pushes it back into the stack twice.
	DupI: {name: "DupI", keyword: "dup_i"},

	// DupD pops a double value from the stack and pushes it back into the stack twice.
	DupD: {name: "DupD", keyword: "dup_d"},

	// RLoadS pops a string from the stack and stores in the target register.
	RLoadS: {name: "RLoadS", keyword: "rload_s", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// RLoadB pops a bool from the stack and stores in the target register.
	RLoadB: {name: "RLoadB", keyword: "rload_b", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// RLoadI pops an integer from the stack and stores in the target register.
	RLoadI: {name: "RLoadI", keyword: "rload_i", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// RLoadD pops a double from the stack and stores in the target register.
	RLoadD: {name: "RLoadD", keyword: "rload_d", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// ALoadS loads its string argument into the target register.
	ALoadS: {name: "ALoadS", keyword: "aload_s", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
		// The string to load.
		OpcodeArgString,
	}},

	// ALoadB loads its boolean argument into the target register.
	ALoadB: {name: "ALoadB", keyword: "aload_b", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
		// The bool to load.
		OpcodeArgBool,
	}},

	// ALoadI loads its integer argument into the target register.
	ALoadI: {name: "ALoadI", keyword: "aload_i", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
		// The integer to load.
		OpcodeArgInt,
	}},

	// ALoadD loads its double argument into the target register.
	ALoadD: {name: "ALoadD", keyword: "aload_d", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
		// The double to load.
		OpcodeArgDouble,
	}},

	// APushS pushes its string argument to the stack.
	APushS: {name: "APushS", keyword: "apush_s", args: []OpcodeArg{
		// The string to push.
		OpcodeArgString,
	}},

	// APushB pushes its boolean argument to the stack.
	APushB: {name: "APushB", keyword: "apush_b", args: []OpcodeArg{
		// The boolean to push.
		OpcodeArgBool,
	}},

	// APushI pushes its integer argument to the stack.
	APushI: {name: "APushI", keyword: "apush_i", args: []OpcodeArg{
		// The integer to push.
		OpcodeArgInt,
	}},

	// APushD pushes its double argument to the stack.
	APushD: {name: "APushD", keyword: "apush_d", args: []OpcodeArg{
		// The double to push.
		OpcodeArgDouble,
	}},

	// RPushS reads a string value from the target register and pushes it into stack.
	RPushS: {name: "RPushS", keyword: "rpush_s", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// RPushB reads a boolean value from the target register and pushes it into stack.
	RPushB: {name: "RPushB", keyword: "rpush_b", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// RPushI reads an integer value from the target register and pushes it into stack.
	RPushI: {name: "RPushI", keyword: "rpush_i", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// RPushD reads a double value from the target register and pushes it into stack.
	RPushD: {name: "RPushD", keyword: "rpush_d", args: []OpcodeArg{
		// The target register.
		OpcodeArgRegister,
	}},

	// EqS pops two string values from the stack and compare for equality. If the strings are
	// equal then it pushes 1 into the stack, otherwise it pushes 0.
	EqS: {name: "EqS", keyword: "eq_s"},

	// EqB pops two bool values from the stack and compare for equality. If the bools are
	// equal then it pushes 1 into the stack, otherwise it pushes 0.
	EqB: {name: "EqB", keyword: "eq_b"},

	// EqI pops two integer values from the stack and compare for equality. If the integers are
	// equal then it pushes 1 into the stack, otherwise it pushes 0.
	EqI: {name: "EqI", keyword: "eq_i"},

	// EqD pops two double values from the stack and compare for equality. If the doubles are
	// equal then it pushes 1 into the stack, otherwise it pushes 0.
	EqD: {name: "EqD", keyword: "eq_d"},

	// AEqS pops a string value from the stack and compare it against its argument for equality.
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqS: {name: "AEqS", keyword: "aeq_s", args: []OpcodeArg{
		// The string value for comparison.
		OpcodeArgString,
	}},

	// AEqB pops a bool value from the stack and compare it against its argument for equality.
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqB: {name: "AEqB", keyword: "aeq_b", args: []OpcodeArg{
		// The bool value for comparison.
		OpcodeArgBool,
	}},

	// AEqI pops an integer value from the stack and compare it against its argument for equality.
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqI: {name: "AEqI", keyword: "aeq_i", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgInt,
	}},

	// AEqD pops a double value from the stack and compare it against its argument for equality.
	// If equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AEqD: {name: "AEqD", keyword: "aeq_d", args: []OpcodeArg{
		// The double value for comparison.
		OpcodeArgDouble,
	}},

	// Xor pops two boolean values from the stack, performs logical exclusive-or, then pushes the
	// result back into stack.
	Xor: {name: "Xor", keyword: "xor"},

	// And pops two boolean values from the stack, performs logical and, then pushes the
	// result back into stack.
	And: {name: "And", keyword: "and"},

	// Or pops two boolean values from the stack, performs logical or, then pushes the
	// result back into stack.
	Or: {name: "Or", keyword: "or"},

	// AXor pops a boolean value from the stack and performs logical exclusive-or with its
	// argument, then pushes the result back into stack.
	AXor: {name: "AXor", keyword: "axor", args: []OpcodeArg{
		// The boolean value to xor.
		OpcodeArgBool,
	}},

	// AAnd pops a boolean value from the stack and performs logical and with its
	// argument, then pushes the result back into stack.
	AAnd: {name: "AAnd", keyword: "aand", args: []OpcodeArg{
		// The boolean value to and.
		OpcodeArgBool,
	}},

	// AOr pops a boolean value from the stack and performs logical or with its
	// argument, then pushes the result back into stack.
	AOr: {name: "AOr", keyword: "aor", args: []OpcodeArg{
		// The boolean value to or.
		OpcodeArgBool,
	}},

	// Not pops a boolean value from the stack and performs logical not,
	// then pushes the result back into stack.
	Not: {name: "Not", keyword: "not"},

	// ResolveS lookups up a string attribute value in the bag, with the given name.
	// If successful, pushes the resolved string into stack, otherwise raises error.
	ResolveS: {name: "ResolveS", keyword: "resolve_s", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// ResolveB lookups up a bool attribute value in the bag, with the given name.
	// If successful, pushes the resolved bool into stack, otherwise raises error.
	ResolveB: {name: "ResolveB", keyword: "resolve_b", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// ResolveI lookups up an integer attribute value in the bag, with the given name.
	// If successful, pushes the resolved integer into stack, otherwise raises error.
	ResolveI: {name: "ResolveI", keyword: "resolve_i", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// ResolveD lookups up a double attribute value in the bag, with the given name.
	// If successful, pushes the resolved double into stack, otherwise raises error.
	ResolveD: {name: "ResolveD", keyword: "resolve_d", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// ResolveF lookups up an interface attribute value in the bag, with the given name.
	// If successful, pushes the resolved interface{} into stack, otherwise raises error.
	ResolveF: {name: "ResolveF", keyword: "resolve_f", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// TResolveS lookups up a string attribute value in the bag, with the given name.
	// If successful, pushes the resolved string value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveS: {name: "TResolveS", keyword: "tresolve_s", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// TResolveB lookups up a  ool attribute value in the bag, with the given name.
	// If successful, pushes the resolved bool value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveB: {name: "TResolveB", keyword: "tresolve_b", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// TResolveI lookups up an integer attribute value in the bag, with the given name.
	// If successful, pushes the resolved integer value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveI: {name: "TResolveI", keyword: "tresolve_i", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// TResolveD lookups up a double attribute value in the bag, with the given name.
	// If successful, pushes the resolved double value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveD: {name: "TResolveD", keyword: "tresolve_d", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// TResolveF lookups up a interface{} attribute value in the bag, with the given name.
	// If successful, pushes the resolved interface{] value, then 1 into the stack,
	// otherwise pushes 0.
	TResolveF: {name: "TResolveF", keyword: "tresolve_f", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// AddI pops two integer values from the stack, adds their value and pushes the result
	// back into stack. The operation follows Go's integer addition semantics.
	AddI: {name: "AddI", keyword: "add_i"},

	// AddD pops two double values from the stack, adds their value and pushes the result
	// back into stack. The operation follows Go's float addition semantics.
	AddD: {name: "AddD", keyword: "add_d"},

	// AddS pops two string values from the stack, adds their value and pushes the result
	// back into stack. The operation follows Go's string concatenation semantics.
	AddS: {name: "AddS", keyword: "add_s"},

	// SubI pops two integer values from the stack, and subtracts the second popped value
	// from the first one, then pushes the result back into stack.
	// The operation follows Go's integer subtraction semantics.
	SubI: {name: "SubI", keyword: "sub_i"},

	// SubD pops two double values from the stack, and subtracts the second popped value
	// from the first one, then pushes the result back into stack.
	// The operation follows Go's float subtraction semantics.
	SubD: {name: "SubD", keyword: "sub_d"},

	// AAddI pops an integer value from the stack, adds the popped value and its argument,
	// and pushes the result back into stack. The operation follows Go's integer addition
	// semantics.
	AAddI: {name: "AAddI", keyword: "aadd_i", args: []OpcodeArg{
		// Value to add
		OpcodeArgInt,
	}},

	// AAddD pops a double value from the stack, adds the popped value and its argument,
	// and pushes the result back into stack. The operation follows Go's double addition
	// semantics.
	AAddD: {name: "AAddD", keyword: "aadd_d", args: []OpcodeArg{
		// Value to add
		OpcodeArgDouble,
	}},

	// ASubI pops an integer value from the stack, subtracts its argument from the popped value,
	// then pushes the result back into stack.  The operation follows Go's integer subtraction
	// semantics.
	ASubI: {name: "ASubI", keyword: "asub_i", args: []OpcodeArg{
		// Value to subtract
		OpcodeArgInt,
	}},

	// ASubD pops a double value from the stack, subtracts its argument from the popped value,
	// then pushes the result back into stack.  The operation follows Go's double subtraction
	// semantics.
	ASubD: {name: "ASubD", keyword: "asub_d", args: []OpcodeArg{
		// Value to subtract
		OpcodeArgDouble,
	}},

	// Jmp jumps to the given instruction address.
	Jmp: {name: "Jmp", keyword: "jmp", args: []OpcodeArg{
		// The address to jump to.
		OpcodeArgAddress,
	}},

	// Jz pops a bool value from the stack. If the value is 0, then jumps to the given
	// instruction address.
	Jz: {name: "Jz", keyword: "jz", args: []OpcodeArg{
		// The address to jump to.
		OpcodeArgAddress,
	}},

	// Jnz pops a bool value from the stack. If the value is not 0, then jumps to the given
	// instruction address.
	Jnz: {name: "Jnz", keyword: "jnz", args: []OpcodeArg{
		// The address to jump to.
		OpcodeArgAddress,
	}},

	// Call invokes the target function.
	Call: {name: "Call", keyword: "call", args: []OpcodeArg{
		// The name of the target function.
		OpcodeArgFunction,
	}},

	// Ret returns from the current function.
	Ret: {name: "Ret", keyword: "ret"},

	// Lookup pops a string, then a stringmap from the stack and perform a lookup on the stringmap
	// using the string as the name. If a value is found, then the value is pushed into the
	// stack.  Otherwise raises an error.
	Lookup: {name: "Lookup", keyword: "lookup"},

	// NLookup pops a string, then a stringmap from the stack and perform a lookup on the stringmap
	// using the string as the name. If a value is found, then the value is pushed into the
	// stack.  Otherwise empty string is pushed ono the stack.
	NLookup: {name: "NLookup", keyword: "nlookup"},

	// TLookup pops a string, then a stringmap from the stack and perform a lookup on the stringmap
	// using the string as the name. If a value is found, then the value is pushed into the
	// stack, then 1.  Otherwise 0 is pushed to into the stack.
	TLookup: {name: "TLookup", keyword: "tlookup"},

	// ALookup pops a stringmap from the stack and perform a lookup on the stringmap using the string
	// parameter as the name. If a value is found, then the value is pushed into the stack
	// Otherwise raises an error.
	ALookup: {name: "ALookup", keyword: "alookup", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// ANLookup pops a stringmap from the stack and perform a lookup on the stringmap using the string
	// parameter as the name. If a value is found, then the value is pushed into the stack
	// Otherwise empty string is pushed onto the stack.
	ANLookup: {name: "ANLookup", keyword: "anlookup", args: []OpcodeArg{
		// The name of the attribute.
		OpcodeArgString,
	}},

	// SizeS pops a string and pushes it length.
	SizeS: {name: "SizeS", keyword: "size_s"},

	// LtS pops two string values from the stack and compare for order.
	// If less then it pushes 1 into the stack, otherwise it pushes 0.
	LtS: {name: "LtS", keyword: "lt_s"},

	// LtI pops two integer values from the stack and compare for order.
	// If less then it pushes 1 into the stack, otherwise it pushes 0.
	LtI: {name: "LtI", keyword: "lt_i"},

	// LtD pops two float values from the stack and compare for order.
	// If less then it pushes 1 into the stack, otherwise it pushes 0.
	LtD: {name: "LtD", keyword: "lt_d"},

	// ALtS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is less, then it pushes 1 into the stack, otherwise it pushes 0.
	ALtS: {name: "ALtS", keyword: "alt_s", args: []OpcodeArg{
		// The string value for comparison.
		OpcodeArgString,
	}},

	// ALtI pops an integer value from the stack and compare it against its argument for order.
	// If the value on the stack is less, then it pushes 1 into the stack, otherwise it pushes 0.
	ALtI: {name: "ALtI", keyword: "alt_i", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgInt,
	}},

	// ALtD pops a float value from the stack and compare it against its argument for order.
	// If the value on the stack is less, then it pushes 1 into the stack, otherwise it pushes 0.
	ALtD: {name: "ALtD", keyword: "alt_d", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgDouble,
	}},

	// LeS pops two string values from the stack and compare for order.
	// If less or equals then it pushes 1 into the stack, otherwise it pushes 0.
	LeS: {name: "LeS", keyword: "le_s"},

	// LeI pops two integer values from the stack and compare for order.
	// If less or equals then it pushes 1 into the stack, otherwise it pushes 0.
	LeI: {name: "LeI", keyword: "le_i"},

	// LeD pops two float values from the stack and compare for order.
	// If less or equals then it pushes 1 into the stack, otherwise it pushes 0.
	LeD: {name: "LeD", keyword: "le_d"},

	// ALeS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is less or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	ALeS: {name: "ALeS", keyword: "ale_s", args: []OpcodeArg{
		// The string value for comparison.
		OpcodeArgString,
	}},

	// ALeI pops an integer value from the stack and compare it against its argument for order.
	// If the value on the stack is less or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	ALeI: {name: "ALeI", keyword: "ale_i", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgInt,
	}},

	// ALeD pops a float value from the stack and compare it against its argument for order.
	// If the value on the stack is less or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	ALeD: {name: "ALeD", keyword: "ale_d", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgDouble,
	}},

	// GtS pops two string values from the stack and compare for order.
	// If greater then it pushes 1 into the stack, otherwise it pushes 0.
	GtS: {name: "GtS", keyword: "gt_s"},

	// GtI pops two integer values from the stack and compare for order.
	// If greater then it pushes 1 into the stack, otherwise it pushes 0.
	GtI: {name: "GtI", keyword: "gt_i"},

	// GtD pops two float values from the stack and compare for order.
	// If greater then it pushes 1 into the stack, otherwise it pushes 0.
	GtD: {name: "GtD", keyword: "gt_d"},

	// AGtS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is greater, then it pushes 1 into the stack, otherwise it pushes 0.
	AGtS: {name: "AGtS", keyword: "agt_s", args: []OpcodeArg{
		// The string value for comparison.
		OpcodeArgString,
	}},

	// AGtI pops an integer value from the stack and compare it against its argument for order.
	// If the value on the stack is greater, then it pushes 1 into the stack, otherwise it pushes 0.
	AGtI: {name: "AGtI", keyword: "agt_i", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgInt,
	}},

	// AGtD pops a float value from the stack and compare it against its argument for order.
	// If the value on the stack is greater, then it pushes 1 into the stack, otherwise it pushes 0.
	AGtD: {name: "AGtD", keyword: "agt_d", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgDouble,
	}},

	// GeS pops two string values from the stack and compare for order.
	// If greater or equal then it pushes 1 into the stack, otherwise it pushes 0.
	GeS: {name: "GeS", keyword: "ge_s"},

	// GeI pops two integer values from the stack and compare for order.
	// If greater or equal then it pushes 1 into the stack, otherwise it pushes 0.
	GeI: {name: "GeI", keyword: "ge_i"},

	// GeD pops two float values from the stack and compare for order.
	// If greater or equal then it pushes 1 into the stack, otherwise it pushes 0.
	GeD: {name: "GeD", keyword: "ge_d"},

	// AGeS pops a string value from the stack and compare it against its argument for order.
	// If the value on the stack is greater or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AGeS: {name: "AGeS", keyword: "age_s", args: []OpcodeArg{
		// The string value for comparison.
		OpcodeArgString,
	}},

	// AGeI pops an integer value from the stack and compare it against its argument for equality.
	// If greater or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AGeI: {name: "AGeI", keyword: "age_i", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgInt,
	}},

	// AGeD pops a float value from the stack and compare it against its argument for equality.
	// If greater or equal, then it pushes 1 into the stack, otherwise it pushes 0.
	AGeD: {name: "AGeD", keyword: "age_d", args: []OpcodeArg{
		// The integer value for comparison.
		OpcodeArgDouble,
	}},
}

var opcodesByKeyword = func() map[string]Opcode {
	r := make(map[string]Opcode)
	for o, i := range opCodeInfos {
		r[i.keyword] = o
	}
	return r
}()

// GetOpcode finds and returns the opcode that matches the keyword text that is supplied.
func GetOpcode(text string) (Opcode, bool) {
	o, f := opcodesByKeyword[text]
	return o, f
}

func (o Opcode) String() string {
	return opCodeInfos[o].name
}

// Size returns the total size the instruction and its arguments occupy, in uint32s.
func (o Opcode) Size() uint32 {
	var s uint32 = 1
	for _, a := range opCodeInfos[o].args {
		s += a.Size()
	}
	return s
}

// Keyword returns the keyword corresponding to the opcode.
func (o Opcode) Keyword() string {
	return opCodeInfos[o].keyword
}

// Args return the metadata about the arguments to the opcode.
func (o Opcode) Args() []OpcodeArg {
	return opCodeInfos[o].args
}

// Size returns the number of uint32 byte codes that the argument occupies.
func (o OpcodeArg) Size() uint32 {
	return argSizes[o]
}
