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

type builderTest struct {
	n string
	i func(*Builder)
	e []uint32
}

var builderTests = []builderTest{
	{
		n: "and",
		i: func(b *Builder) {
			b.And()
		},
		e: []uint32{
			uint32(And),
		},
	},
	{
		n: "xor",
		i: func(b *Builder) {
			b.Xor()
		},
		e: []uint32{
			uint32(Xor),
		},
	},
	{
		n: "not",
		i: func(b *Builder) {
			b.Not()
		},
		e: []uint32{
			uint32(Not),
		},
	},
	{
		n: "or",
		i: func(b *Builder) {
			b.Or()
		},
		e: []uint32{
			uint32(Or),
		},
	},
	{
		n: "eqstring",
		i: func(b *Builder) {
			b.EQString()
		},
		e: []uint32{
			uint32(EqS),
		},
	},
	{
		n: "aeqstring",
		i: func(b *Builder) {
			b.AEQString("abv")
		},
		e: []uint32{
			uint32(AEqS),
			1, //str index
		},
	},
	{
		n: "eqinteger",
		i: func(b *Builder) {
			b.EQInteger()
		},
		e: []uint32{
			uint32(EqI),
		},
	},
	{
		n: "aeqinteger",
		i: func(b *Builder) {
			b.AEQInteger(345)
		},
		e: []uint32{
			uint32(AEqI),
			345,
			0,
		},
	},
	{
		n: "eqbool",
		i: func(b *Builder) {
			b.EQBool()
		},
		e: []uint32{
			uint32(EqB),
		},
	},
	{
		n: "aeqbool",
		i: func(b *Builder) {
			b.AEQBool(true)
		},
		e: []uint32{
			uint32(AEqB),
			1,
		},
	},
	{
		n: "eqdouble",
		i: func(b *Builder) {
			b.EQDouble()
		},
		e: []uint32{
			uint32(EqD),
		},
	},
	{
		n: "aeqdouble",
		i: func(b *Builder) {
			b.AEQDouble(10.123)
		},
		e: []uint32{
			uint32(AEqD),
			3676492005,
			1076117241,
		},
	},
	{
		n: "ret",
		i: func(b *Builder) {
			b.Ret()
		},
		e: []uint32{
			uint32(Ret),
		},
	},
	{
		n: "call",
		i: func(b *Builder) {
			b.Call("foo")
		},
		e: []uint32{
			uint32(Call),
			1, //str index
		},
	},
	{
		n: "resolveint",
		i: func(b *Builder) {
			b.ResolveInt("foo")
		},
		e: []uint32{
			uint32(ResolveI),
			1, //str index
		},
	},
	{
		n: "tresolveint",
		i: func(b *Builder) {
			b.TResolveInt("foo")
		},
		e: []uint32{
			uint32(TResolveI),
			1, //str index
		},
	},
	{
		n: "resolvebool",
		i: func(b *Builder) {
			b.ResolveBool("foo")
		},
		e: []uint32{
			uint32(ResolveB),
			1, //str index
		},
	},
	{
		n: "tresolvebool",
		i: func(b *Builder) {
			b.TResolveBool("foo")
		},
		e: []uint32{
			uint32(TResolveB),
			1, //str index
		},
	},
	{
		n: "resolvestring",
		i: func(b *Builder) {
			b.ResolveString("foo")
		},
		e: []uint32{
			uint32(ResolveS),
			1, //str index
		},
	},
	{
		n: "tresolvestring",
		i: func(b *Builder) {
			b.TResolveString("foo")
		},
		e: []uint32{
			uint32(TResolveS),
			1, //str index
		},
	},
	{
		n: "resolvedouble",
		i: func(b *Builder) {
			b.ResolveDouble("foo")
		},
		e: []uint32{
			uint32(ResolveD),
			1, //str index
		},
	},
	{
		n: "tresolvedouble",
		i: func(b *Builder) {
			b.TResolveDouble("foo")
		},
		e: []uint32{
			uint32(TResolveD),
			1, //str index
		},
	},
	{
		n: "resolveinterface",
		i: func(b *Builder) {
			b.ResolveInterface("foo")
		},
		e: []uint32{
			uint32(ResolveF),
			1, //str index
		},
	},
	{
		n: "tresolveinterface",
		i: func(b *Builder) {
			b.TResolveInterface("foo")
		},
		e: []uint32{
			uint32(TResolveF),
			1, //str index
		},
	},
	{
		n: "apushbool",
		i: func(b *Builder) {
			b.APushBool(true)
		},
		e: []uint32{
			uint32(APushB),
			1,
		},
	},
	{
		n: "apushstr",
		i: func(b *Builder) {
			b.APushStr("ABV")
		},
		e: []uint32{
			uint32(APushS),
			1, //str index
		},
	},
	{
		n: "apushdouble",
		i: func(b *Builder) {
			b.APushDouble(123.456)
		},
		e: []uint32{
			uint32(APushD),
			446676599,
			1079958831,
		},
	},
	{
		n: "apushint",
		i: func(b *Builder) {
			b.APushInt(456)
		},
		e: []uint32{
			uint32(APushI),
			456,
			0,
		},
	},
	{
		n: "lookup",
		i: func(b *Builder) {
			b.Lookup()
		},
		e: []uint32{
			uint32(Lookup),
		},
	},
	{
		n: "nlookup",
		i: func(b *Builder) {
			b.NLookup()
		},
		e: []uint32{
			uint32(NLookup),
		},
	},
	{
		n: "alookup",
		i: func(b *Builder) {
			b.ALookup("abc")
		},
		e: []uint32{
			uint32(ALookup),
			1, //str index
		},
	},
	{
		n: "anlookup",
		i: func(b *Builder) {
			b.ANLookup("abc")
		},
		e: []uint32{
			uint32(ANLookup),
			1, //str index
		},
	},
	{
		n: "tlookup",
		i: func(b *Builder) {
			b.TLookup()
		},
		e: []uint32{
			uint32(TLookup),
		},
	},
	{
		n: "labels",
		i: func(b *Builder) {
			l := b.AllocateLabel()
			b.Nop()
			b.Jmp(l)
			b.Nop()
			b.Jnz(l)
			b.Nop()
			b.Jz(l)
			b.Nop()
			b.SetLabelPos(l)
			b.Nop()
			b.Jmp(l)
			b.Nop()
			b.Jnz(l)
			b.Nop()
			b.Jz(l)
			b.Nop()
			b.Jmp("LLL")
			b.SetLabelPos("LLL")
			b.Nop()
		},
		e: []uint32{
			uint32(Nop),
			uint32(Jmp),
			10, // label address
			uint32(Nop),
			uint32(Jnz),
			10, // label address
			uint32(Nop),
			uint32(Jz),
			10, // label address
			uint32(Nop),
			uint32(Nop), // label points to here
			uint32(Jmp),
			10, // label address
			uint32(Nop),
			uint32(Jnz),
			10, // label address
			uint32(Nop),
			uint32(Jz),
			10, // label address
			uint32(Nop),
			uint32(Jmp),
			22,
			uint32(Nop),
		},
	},
}

func Test(t *testing.T) {
	for _, test := range builderTests {
		t.Run(test.n, func(tt *testing.T) {
			p := NewProgram()
			b := NewBuilder(p.strings)
			test.i(b)
			body := b.Build()

			same := (len(body) == len(test.e))
			if same {
				for i := 0; i < len(body); i++ {
					if body[i] != test.e[i] {
						same = false
						break
					}
				}
			}

			if !same {
				tt.Logf("Expected:\n%v\n", test.e)
				tt.Logf("Actual:\n%v\n", body)
				tt.Fatal()
			}
		})
	}
}

func TestSetLabelPosTwicePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("The code did not panic")
		}
	}()

	p := NewProgram()
	b := NewBuilder(p.strings)
	b.SetLabelPos("l")
	b.SetLabelPos("l") // Try setting again.
}
