// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package interpreter

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types/ref"
)

// Instruction represents a single step within a CEL program.
type Instruction interface {
	fmt.Stringer
	GetID() int64
}

type baseInstruction struct {
	ID int64
}

// GetID returns the Expr id for the instruction.
func (e *baseInstruction) GetID() int64 {
	return e.ID
}

// ConstExpr is a constant expression.
type ConstExpr struct {
	*baseInstruction
	Value ref.Value
}

// String generates pseudo-assembly for the instruction.
func (e *ConstExpr) String() string {
	return fmt.Sprintf("const %v, r%d", e.Value, e.GetID())
}

// NewLiteral generates a ConstExpr.
func NewLiteral(exprID int64, value ref.Value) *ConstExpr {
	return &ConstExpr{&baseInstruction{exprID}, value}
}

// IdentExpr is an identifier expression.
type IdentExpr struct {
	*baseInstruction
	Name string
}

// String generates pseudo-assembly for the instruction.
func (e *IdentExpr) String() string {
	return fmt.Sprintf("local '%s', r%d", e.Name, e.GetID())
}

// NewIdent generates an IdentExpr.
func NewIdent(exprID int64, name string) *IdentExpr {
	return &IdentExpr{&baseInstruction{exprID}, name}
}

// CallExpr is a call expression where the args are referenced by id.
type CallExpr struct {
	*baseInstruction
	Function string
	Args     []int64
	Overload string
	Strict   bool
}

// String generates pseudo-assembly for the instruction.
func (e *CallExpr) String() string {
	argRegs := make([]string, len(e.Args), len(e.Args))
	for i, arg := range e.Args {
		argRegs[i] = fmt.Sprintf("r%d", arg)
	}
	return fmt.Sprintf("call  %s(%v), r%d",
		e.Function,
		strings.Join(argRegs, ", "),
		e.GetID())
}

// NewCall generates a CallExpr for non-overload calls.
func NewCall(exprID int64, function string, argIDs []int64) *CallExpr {
	return &CallExpr{&baseInstruction{exprID}, function, argIDs, "", checkIsStrict(function)}
}

// NewCallOverload generates a CallExpr for overload calls.
func NewCallOverload(exprID int64, function string, argIDs []int64, overload string) *CallExpr {
	return &CallExpr{&baseInstruction{exprID}, function, argIDs, overload, checkIsStrict(function)}
}

func checkIsStrict(function string) bool {
	if function != operators.LogicalAnd && function != operators.LogicalOr && function != operators.Conditional {
		return true
	}
	return false
}

// SelectExpr is a select expression where the operand is represented by id.
type SelectExpr struct {
	*baseInstruction
	Operand int64
	Field   string
}

// String generates pseudo-assembly for the instruction.
func (e *SelectExpr) String() string {
	return fmt.Sprintf("call  select(%d, '%s'), r%d",
		e.Operand, e.Field, e.GetID())
}

// NewSelect generates a SelectExpr.
func NewSelect(exprID int64, operandID int64, field string) *SelectExpr {
	return &SelectExpr{&baseInstruction{exprID}, operandID, field}
}

// CreateListExpr will create a new list from the elements referened by their ids.
type CreateListExpr struct {
	*baseInstruction
	Elements []int64
}

// String generates pseudo-assembly for the instruction.
func (e *CreateListExpr) String() string {
	return fmt.Sprintf("mov   list(%v), r%d", e.Elements, e.GetID())
}

// NewList generates a CreateListExpr.
func NewList(exprID int64, elements []int64) *CreateListExpr {
	return &CreateListExpr{&baseInstruction{exprID}, elements}
}

// CreateMapExpr will create a map from the key value pairs where each key and
// value refers to an expression id.
type CreateMapExpr struct {
	*baseInstruction
	KeyValues map[int64]int64
}

// String generates pseudo-assembly for the instruction.
func (e *CreateMapExpr) String() string {
	return fmt.Sprintf("mov   map(%v), r%d", e.KeyValues, e.GetID())
}

// NewMap generates a CreateMapExpr.
func NewMap(exprID int64, keyValues map[int64]int64) *CreateMapExpr {
	return &CreateMapExpr{&baseInstruction{exprID}, keyValues}
}

// CreateObjectExpr generates a new typed object with field values referenced
// by id.
type CreateObjectExpr struct {
	*baseInstruction
	Name        string
	FieldValues map[string]int64
}

// String generates pseudo-assembly for the instruction.
func (e *CreateObjectExpr) String() string {
	return fmt.Sprintf("mov   type(%s%v), r%d", e.Name, e.FieldValues, e.GetID())
}

// NewObject generates a CreateObjectExpr.
func NewObject(exprID int64, name string,
	fieldValues map[string]int64) *CreateObjectExpr {
	return &CreateObjectExpr{&baseInstruction{exprID}, name, fieldValues}
}

// JumpInst represents an conditional jump to an instruction offset.
type JumpInst struct {
	*baseInstruction
	Count       int
	OnCondition func(EvalState) bool
}

// String generates pseudo-assembly for the instruction.
func (e *JumpInst) String() string {
	return fmt.Sprintf("jump  %d if cond<r%d>", e.Count, e.GetID())
}

// NewJump generates a JumpInst.
func NewJump(exprID int64, instructionCount int, cond func(EvalState) bool) *JumpInst {
	return &JumpInst{
		baseInstruction: &baseInstruction{exprID},
		Count:           instructionCount,
		OnCondition:     cond}
}

// MovInst assigns the value of one expression id to another.
type MovInst struct {
	*baseInstruction
	ToExprID int64
}

// String generates pseudo-assembly for the instruction.
func (e *MovInst) String() string {
	return fmt.Sprintf("mov   r%d, r%d", e.GetID(), e.ToExprID)
}

// NewMov generates a MovInst.
func NewMov(exprID int64, toExprID int64) *MovInst {
	return &MovInst{&baseInstruction{exprID}, toExprID}
}

// PushScopeInst results in the generation of a new Activation containing the values
// of the associated declarations.
type PushScopeInst struct {
	*baseInstruction
	Declarations map[string]int64
}

// String generates pseudo-assembly for the instruction.
func (e *PushScopeInst) String() string {
	return fmt.Sprintf("block  %v", e.Declarations)
}

// NewPushScope generates a PushScopeInst.
func NewPushScope(exprID int64, declarations map[string]int64) *PushScopeInst {
	return &PushScopeInst{&baseInstruction{exprID}, declarations}
}

// PopScopeInst resets the current activation to the Activation#Parent() of the
// previous activation.
type PopScopeInst struct {
	*baseInstruction
}

// String generates pseudo-assembly for the instruction.
func (e *PopScopeInst) String() string {
	return fmt.Sprintf("end")
}

// NewPopScope generates a PopScopeInst.
func NewPopScope(exprID int64) *PopScopeInst {
	return &PopScopeInst{&baseInstruction{exprID}}
}
