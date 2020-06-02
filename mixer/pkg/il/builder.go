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
	"fmt"
)

// Builder is used for building function bodies in a programmatic way.
type Builder struct {
	strings *StringTable
	body    []uint32
	labels  map[string]uint32
	fixups  map[string][]uint32
}

// NewBuilder creates a new Builder instance. It uses the given StringTable when it needs to
// allocate ids for strings.
func NewBuilder(s *StringTable) *Builder {
	return &Builder{
		strings: s,
		body:    make([]uint32, 0, 32),

		// labels maps the label name to the target address. If the position is not set yet, then
		// the label position should be marked as 0, and any usages of the labels needs to be
		// fixed up.
		labels: make(map[string]uint32),

		// fixups holds the bytecode locations that needs to be updated once a label position is set.
		fixups: make(map[string][]uint32),
	}
}

// Build completes building a function body and returns the accumulated byte code.
func (f *Builder) Build() []uint32 {
	return f.body
}

// Nop appends the "nop" instruction to the byte code.
func (f *Builder) Nop() {
	f.op0(Nop)
}

// Ret appends the "ret" instruction to the byte code.
func (f *Builder) Ret() {
	f.op0(Ret)
}

// Call appends the "call" instruction to the byte code.
func (f *Builder) Call(fnName string) {
	f.op1(Call, f.id(fnName))
}

// ResolveInt appends the "resolve_i" instruction to the byte code.
func (f *Builder) ResolveInt(n string) {
	f.op1(ResolveI, f.id(n))
}

// TResolveInt appends the "tresolve_i" instruction to the byte code.
func (f *Builder) TResolveInt(n string) {
	f.op1(TResolveI, f.id(n))
}

// ResolveString appends the "resolve_s" instruction to the byte code.
func (f *Builder) ResolveString(n string) {
	f.op1(ResolveS, f.id(n))
}

// TResolveString appends the "tresolve_s" instruction to the byte code.
func (f *Builder) TResolveString(n string) {
	f.op1(TResolveS, f.id(n))
}

// ResolveBool appends the "resolve_b" instruction to the byte code.
func (f *Builder) ResolveBool(n string) {
	f.op1(ResolveB, f.id(n))
}

// TResolveBool appends the "tresolve_b" instruction to the byte code.
func (f *Builder) TResolveBool(n string) {
	f.op1(TResolveB, f.id(n))
}

// ResolveDouble appends the "resolve_d" instruction to the byte code.
func (f *Builder) ResolveDouble(n string) {
	f.op1(ResolveD, f.id(n))
}

// TResolveDouble appends the "tresolve_d" instruction to the byte code.
func (f *Builder) TResolveDouble(n string) {
	f.op1(TResolveD, f.id(n))
}

// ResolveInterface appends the "resolve_f" instruction to the byte code.
func (f *Builder) ResolveInterface(n string) {
	f.op1(ResolveF, f.id(n))
}

// TResolveInterface appends the "tresolve_f" instruction to the byte code.
func (f *Builder) TResolveInterface(n string) {
	f.op1(TResolveF, f.id(n))
}

// APushBool appends the "apush_b" instruction to the byte code.
func (f *Builder) APushBool(b bool) {
	f.op1(APushB, BoolToByteCode(b))
}

// APushStr appends the "apush_s" instruction to the byte code.
func (f *Builder) APushStr(s string) {
	f.op1(APushS, f.id(s))
}

// APushInt appends the "apush_i" instruction to the byte code.
func (f *Builder) APushInt(i int64) {
	a1, a2 := IntegerToByteCode(i)
	f.op2(APushI, a1, a2)
}

// APushDouble appends the "apush_d" instruction to the byte code.
func (f *Builder) APushDouble(n float64) {
	a1, a2 := DoubleToByteCode(n)
	f.op2(APushD, a1, a2)
}

// Xor appends the "xor" instruction to the byte code.
func (f *Builder) Xor() {
	f.op0(Xor)
}

// EQString appends the "eq_s" instruction to the byte code.
func (f *Builder) EQString() {
	f.op0(EqS)
}

// AEQString appends the "ieq_s" instruction to the byte code.
func (f *Builder) AEQString(v string) {
	f.op1(AEqS, f.id(v))
}

// LTString appends the "lt_s" instruction to the byte code.
func (f *Builder) LTString() {
	f.op0(LtS)
}

// LTInteger appends the "lt_i" instruction to the byte code.
func (f *Builder) LTInteger() {
	f.op0(LtI)
}

// LTDouble appends the "lt_d" instruction to the byte code.
func (f *Builder) LTDouble() {
	f.op0(LtD)
}

// ALTString appends the "alt_s" instruction to the byte code.
func (f *Builder) ALTString(v string) {
	f.op1(ALtS, f.id(v))
}

// ALTInteger appends the "alt_i" instruction to the byte code.
func (f *Builder) ALTInteger(v int64) {
	a1, a2 := IntegerToByteCode(v)
	f.op2(ALtI, a1, a2)
}

// ALTDouble appends the "alt_d" instruction to the byte code.
func (f *Builder) ALTDouble(v float64) {
	a1, a2 := DoubleToByteCode(v)
	f.op2(ALtD, a1, a2)
}

// LEString appends the "le_s" instruction to the byte code.
func (f *Builder) LEString() {
	f.op0(LeS)
}

// LEInteger appends the "le_i" instruction to the byte code.
func (f *Builder) LEInteger() {
	f.op0(LeI)
}

// LEDouble appends the "le_d" instruction to the byte code.
func (f *Builder) LEDouble() {
	f.op0(LeD)
}

// ALEString appends the "ale_s" instruction to the byte code.
func (f *Builder) ALEString(v string) {
	f.op1(ALeS, f.id(v))
}

// ALEInteger appends the "ale_i" instruction to the byte code.
func (f *Builder) ALEInteger(v int64) {
	a1, a2 := IntegerToByteCode(v)
	f.op2(ALeI, a1, a2)
}

// ALEDouble appends the "ale_d" instruction to the byte code.
func (f *Builder) ALEDouble(v float64) {
	a1, a2 := DoubleToByteCode(v)
	f.op2(ALeD, a1, a2)
}

// GTString appends the "gt_s" instruction to the byte code.
func (f *Builder) GTString() {
	f.op0(GtS)
}

// GTInteger appends the "gt_i" instruction to the byte code.
func (f *Builder) GTInteger() {
	f.op0(GtI)
}

// GTDouble appends the "gt_d" instruction to the byte code.
func (f *Builder) GTDouble() {
	f.op0(GtD)
}

// AGTString appends the "agt_s" instruction to the byte code.
func (f *Builder) AGTString(v string) {
	f.op1(AGtS, f.id(v))
}

// AGTInteger appends the "agt_i" instruction to the byte code.
func (f *Builder) AGTInteger(v int64) {
	a1, a2 := IntegerToByteCode(v)
	f.op2(AGtI, a1, a2)
}

// AGTDouble appends the "agt_d" instruction to the byte code.
func (f *Builder) AGTDouble(v float64) {
	a1, a2 := DoubleToByteCode(v)
	f.op2(AGtD, a1, a2)
}

// GEString appends the "ge_s" instruction to the byte code.
func (f *Builder) GEString() {
	f.op0(GeS)
}

// GEInteger appends the "ge_i" instruction to the byte code.
func (f *Builder) GEInteger() {
	f.op0(GeI)
}

// GEDouble appends the "ge_d" instruction to the byte code.
func (f *Builder) GEDouble() {
	f.op0(GeD)
}

// AGEString appends the "age_s" instruction to the byte code.
func (f *Builder) AGEString(v string) {
	f.op1(AGeS, f.id(v))
}

// AGEInteger appends the "age_d" instruction to the byte code.
func (f *Builder) AGEInteger(v int64) {
	a1, a2 := IntegerToByteCode(v)
	f.op2(AGeI, a1, a2)
}

// AGEDouble appends the "age_i" instruction to the byte code.
func (f *Builder) AGEDouble(v float64) {
	a1, a2 := DoubleToByteCode(v)
	f.op2(AGeD, a1, a2)
}

// EQBool appends the "eq_b" instruction to the byte code.
func (f *Builder) EQBool() {
	f.op0(EqB)
}

// AEQBool appends the "ieq_b" instruction to the byte code.
func (f *Builder) AEQBool(v bool) {
	f.op1(AEqB, BoolToByteCode(v))
}

// EQInteger appends the "eq_i" instruction to the byte code.
func (f *Builder) EQInteger() {
	f.op0(EqI)
}

// AEQInteger appends the "eq_i" instruction to the byte code.
func (f *Builder) AEQInteger(v int64) {
	a1, a2 := IntegerToByteCode(v)
	f.op2(AEqI, a1, a2)
}

// EQDouble appends the "eq_d" instruction to the byte code.
func (f *Builder) EQDouble() {
	f.op0(EqD)
}

// AEQDouble appends the "ieq_d" instruction to the byte code.
func (f *Builder) AEQDouble(v float64) {
	a1, a2 := DoubleToByteCode(v)
	f.op2(AEqD, a1, a2)
}

// Not appends the "not" instruction to the byte code.
func (f *Builder) Not() {
	f.op0(Not)
}

// Or appends the "or" instruction to the byte code.
func (f *Builder) Or() {
	f.op0(Or)
}

// And appends the "and" instruction to the byte code.
func (f *Builder) And() {
	f.op0(And)
}

// Lookup appends the "lookup" instruction to the byte code.
func (f *Builder) Lookup() {
	f.op0(Lookup)
}

// NLookup appends the "nlookup" instruction to the byte code.
func (f *Builder) NLookup() {
	f.op0(NLookup)
}

// TLookup appends the "tlookup" instruction to the byte code.
func (f *Builder) TLookup() {
	f.op0(TLookup)
}

// ALookup appends the "alookup" instruction to the byte code.
func (f *Builder) ALookup(v string) {
	f.op1(ALookup, f.id(v))
}

// ANLookup appends the "anlookup" instruction to the byte code.
func (f *Builder) ANLookup(v string) {
	f.op1(ANLookup, f.id(v))
}

// AllocateLabel allocates a new label value for use within the code.
func (f *Builder) AllocateLabel() string {
	l := fmt.Sprintf("L%d", len(f.labels)+len(f.fixups))
	f.fixups[l] = []uint32{}
	return l
}

// SetLabelPos puts the label position at the current bytecode point that builder is pointing at.
// Panics if the label position was already set.
func (f *Builder) SetLabelPos(label string) {
	if _, exists := f.labels[label]; exists {
		panic("il.Builder: setting the label position twice.")
	}
	adr := uint32(len(f.body))
	f.labels[label] = adr
	if fixups, found := f.fixups[label]; found {
		for _, fixup := range fixups {
			f.body[fixup] = adr
		}
		delete(f.fixups, label)
	}
}

// Jz appends the "jz" instruction to the byte code, against the given label.
func (f *Builder) Jz(label string) {
	f.jump(Jz, label)
}

// Jnz appends the "jnz" instruction to the byte code, against the given label.
func (f *Builder) Jnz(label string) {
	f.jump(Jnz, label)
}

// Jmp appends the "jmp" instruction to the byte code, against the given label.
func (f *Builder) Jmp(label string) {
	f.jump(Jmp, label)
}

// AddString appends the "add_s" instruction to the byte code.
func (f *Builder) AddString() {
	f.op0(AddS)
}

// AddDouble appends the "add_d" instruction to the byte code.
func (f *Builder) AddDouble() {
	f.op0(AddD)
}

// AddInteger appends the "add_i" instruction to the byte code.
func (f *Builder) AddInteger() {
	f.op0(AddI)
}

// SizeString appends the "size_s" instruction to the byte code.
func (f *Builder) SizeString() {
	f.op0(SizeS)
}

func (f *Builder) jump(op Opcode, label string) {
	adr, exists := f.labels[label]
	if !exists {
		adr = 0
	}

	f.op1(op, adr)

	if adr == 0 {
		fixup := uint32(len(f.body) - 1)
		f.fixups[label] = append(f.fixups[label], fixup)
	}
}

// op0 inserts a 0-arg opcode into the body.
func (f *Builder) op0(op Opcode) {
	f.body = append(f.body, uint32(op))
}

// op1 inserts a 1-arg opcode into the body.
func (f *Builder) op1(op Opcode, p1 uint32) {
	f.body = append(f.body, uint32(op), p1)
}

// op2 inserts a 2-arg opcode into the body.
func (f *Builder) op2(op Opcode, p1 uint32, p2 uint32) {
	f.body = append(f.body, uint32(op), p1, p2)
}

func (f *Builder) id(s string) uint32 {
	return f.strings.Add(s)
}
