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

package il

import (
	"testing"
)

func TestAddExternDef(t *testing.T) {
	p := NewProgram()
	p.AddExternDef("ext", []Type{}, String)
	f := p.Functions.Get("ext")

	if f.Address != 0 {
		t.Fatal("Address should have been 0")
	}

	if f.Length != 0 {
		t.Fatal("Length should have been 0")
	}

	if f.ReturnType != String {
		t.Fatal("Return type should have been a string")
	}
}

func TestAddFunction(t *testing.T) {
	p := NewProgram()
	b := NewBuilder(p.Strings())
	b.Nop()
	b.SetLabelPos("L1")
	b.Xor()
	b.Jmp("L1")
	b.Ret()

	if err := p.AddFunction("f", []Type{}, Bool, b.Build()); err != nil {
		t.Fatal("The function should have been scribed correctly.")
	}

	expected := []uint32{
		uint32(Halt),
		uint32(Halt), // Preamble halts
		uint32(Nop),
		uint32(Xor),
		uint32(Jmp),
		3, // target address
		uint32(Ret),
	}

	actual := p.ByteCode()
	if len(expected) != len(actual) {
		t.Logf("Expected: %v\n", expected)
		t.Logf("Actual:   %v\n", actual)
		t.Fatal("Expected size does not match actual size")
	}

	for i := 0; i < len(expected); i++ {
		if expected[i] != actual[i] {
			t.Logf("Expected: %v\n", expected)
			t.Logf("Actual:   %v\n", actual)
			t.Fatal("Expected does not match actual")

		}
	}
}

func TestInvalidByteCode(t *testing.T) {
	p := NewProgram()
	body := []uint32{
		uint32(Jmp),
	}

	if err := p.AddFunction("f", []Type{}, Bool, body); err == nil {
		t.Fatal("The function should have returned error.")
	}
}
