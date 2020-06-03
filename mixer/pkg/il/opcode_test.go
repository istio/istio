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

import (
	"testing"
)

func TestGetOpcodeSuccess(t *testing.T) {
	for k, v := range opcodesByKeyword {
		e, f := GetOpcode(k)
		if !f {
			t.Fatal("Should have found the opcode")
		}
		if e != v {
			t.Fatalf("Value mismatch: E:%v, A:%v", e, v)
		}
	}
}

func TestGetOpcodeFailure(t *testing.T) {
	_, f := GetOpcode("not an opcode")
	if f {
		t.Fatal("Shouldn't have found the opcode")
	}
}

func TestOpcodeString(t *testing.T) {
	for k, v := range opCodeInfos {
		if k.String() != v.name {
			t.Fatal("Should have returned the name")
		}
	}
}

func TestOpcodeKeyword(t *testing.T) {
	for k, v := range opCodeInfos {
		if k.Keyword() != v.keyword {
			t.Fatal("Should have returned the keyword")
		}
	}
}

func TestOpcodeArgs(t *testing.T) {
	for k, v := range opCodeInfos {
		if len(k.Args()) != len(v.args) {
			t.Fatal("Should have returned the arguments")
		}
		for i := 0; i < len(k.Args()); i++ {
			if k.Args()[i] != v.args[i] {
				t.Fatal("Should have returned the arguments")
			}
		}
	}
}

func TestOpcodeArgsSize(t *testing.T) {
	for k, v := range argSizes {
		if v != k.Size() {
			t.Fatal("Should have returned the size")
		}
	}
}

func TestOpcodeSize(t *testing.T) {
	// Just test one example.
	if ALoadD.Size() != 1+OpcodeArgDouble.Size()+OpcodeArgRegister.Size() {
		t.Fatal("Should have returned the right size.")
	}
}
