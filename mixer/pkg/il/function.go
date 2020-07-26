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

// Function contains metadata about an IL-based function that is implemented in a program.
type Function struct {

	// Parameters is the type of the input parameters to the function.
	Parameters []Type

	// ReturnType is the return type of the function.
	ReturnType Type

	// ID is the id of the function. It is also the id of the name of the function in the strings table.
	ID uint32

	// Address is the address of the first opcode for the function within the bytecode.
	Address uint32

	// Length is the length of the function, in uint32s, within the bytecode.
	Length uint32
}

// FunctionTable contains a set of functions, organized by their ids.
type FunctionTable struct {
	// The list of functions by the string id of their name.
	functions map[uint32]*Function

	// The strings table that is the basis of id-string mapping for function names.
	strings *StringTable
}

// newFunctionTable creates a new FunctionTable and returns.
func newFunctionTable(strings *StringTable) *FunctionTable {
	return &FunctionTable{
		functions: make(map[uint32]*Function),
		strings:   strings,
	}
}

func (t *FunctionTable) add(f *Function) {
	t.functions[f.ID] = f
}

// Names returns the names of all the functions in the table.
func (t *FunctionTable) Names() []string {
	r := make([]string, len(t.functions))
	i := 0
	for _, f := range t.functions {
		r[i] = t.strings.GetString(f.ID)
		i++
	}
	return r
}

// Get returns the function with the given name if it exists, or nil if not found.
func (t *FunctionTable) Get(name string) *Function {

	id := t.strings.TryGetID(name)
	if id == 0 {
		return nil
	}

	return t.functions[id]
}

// GetByID returns the function with the given id, or nil if not found.
func (t *FunctionTable) GetByID(id uint32) *Function {
	return t.functions[id]
}

// IDOf returns the id of a function with the given name, if it exists. Otherwise it returns 0.
func (t *FunctionTable) IDOf(name string) uint32 {
	return t.strings.TryGetID(name)
}
