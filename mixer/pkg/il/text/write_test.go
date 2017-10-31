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

package text

import (
	"math"
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/il"
)

func checkWrites(t *testing.T, p *il.Program, expected string) {
	actual := WriteText(p)

	if strings.TrimSpace(actual) != strings.TrimSpace(expected) {
		t.Logf("Expected:\n%s\n\n", expected)
		t.Logf("Actual:\n%s\n\n", actual)
		t.Fail()
		return
	}
}

func TestWriteEmptyProgram(t *testing.T) {
	p := il.NewProgram()

	checkWrites(t, p, ``)
}

func TestWriteEmptyFunction(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() bool
end
	`)
}

func TestWriteEmptyFunctionWithParameters(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{il.Interface}, il.Bool, []uint32{})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main(interface) bool
end
	`)
}

func TestWriteTwoFunctionsWithParameters(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("foo", []il.Type{il.Bool}, il.Bool, []uint32{})
	if err != nil {
		t.Error(err)
		return
	}
	err = p.AddFunction("bar", []il.Type{il.Integer, il.String}, il.Bool, []uint32{})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn bar(integer string) bool
end

fn foo(bool) bool
end
	`)
}

func TestWriteBasicFunction(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{
		uint32(il.Ret),
	})
	if err != nil {
		t.Error(err)
		return
	}

	checkWrites(t, p, `
fn main() bool
  ret
end
	`)
}

func TestWriteStringArg(t *testing.T) {
	p := il.NewProgram()
	id := p.Strings().GetID("foo")
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{
		uint32(il.ResolveS),
		uint32(id),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() bool
  resolve_s "foo"
end
	`)
}

func TestWriteLabelArg(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{
		uint32(il.Jnz),
		uint32(0),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() bool
L0:
  jnz L0
end
	`)
}

func TestWriteLabelArg2(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{
		uint32(il.Halt),
		uint32(il.Jnz),
		uint32(1),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() bool
  halt
L0:
  jnz L0
end
	`)
}

func TestWriteFunctionArg(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("callee", []il.Type{}, il.String, []uint32{
		uint32(il.Ret),
	})
	if err != nil {
		t.Error(err)
		return
	}
	err = p.AddFunction("caller", []il.Type{}, il.Bool, []uint32{
		uint32(il.Call),
		p.Strings().GetID("callee"),
		uint32(il.Ret),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn callee() string
  ret
end

fn caller() bool
  call callee
  ret
end
	`)
}

func TestWriteRegisterArg(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Void, []uint32{
		uint32(il.RLoadB),
		uint32(1),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() void
  rload_b r1
end
	`)
}

func TestWriteIntArg(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{
		uint32(il.APushI),
		uint32(1),
		uint32(0),
		uint32(il.APushI),
		uint32(2),
		uint32(0),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() bool
  apush_i 1
  apush_i 2
end
	`)
}

func TestWriteFloatArg(t *testing.T) {
	fl := float64(123.456)
	ui := math.Float64bits(fl)
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Bool, []uint32{
		uint32(il.APushD),
		uint32(ui & 0xFFFFFFFF),
		uint32(ui >> 32),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() bool
  apush_d 123.456000
end
	`)
}

func TestWriteBoolArg(t *testing.T) {
	p := il.NewProgram()
	err := p.AddFunction("main", []il.Type{}, il.Integer, []uint32{
		uint32(il.APushB),
		uint32(1),
		uint32(il.APushB),
		uint32(0),
	})
	if err != nil {
		t.Error(err)
		return
	}
	checkWrites(t, p, `
fn main() integer
  apush_b true
  apush_b false
end
	`)
}
