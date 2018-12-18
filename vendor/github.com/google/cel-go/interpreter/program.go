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

	"github.com/google/cel-go/common"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Program contains instructions and related metadata.
type Program interface {
	// Begin returns an InstructionStepper which iterates through the
	// instructions. Each call to Begin() returns a stepper that starts
	// from the first instructions.
	//
	// Note: Init() must be called prior to Begin().
	Begin() InstructionStepper

	// GetInstruction returns the instruction at the given runtime expression id.
	GetInstruction(runtimeID int64) Instruction

	// Init ensures that instructions have been properly initialized prior to
	// beginning the execution of a program. The init step may optimize the
	// instruction set.
	Init(dispatcher Dispatcher, state MutableEvalState)

	// MaxInstructionID returns the identifier of the last expression in the
	// program.
	MaxInstructionID() int64

	// Metadata used to determine source locations of sub-expressions.
	Metadata() Metadata
}

// InstructionStepper steps through program instructions and provides an option
// to jump a certain number of instructions forward or back.
type InstructionStepper interface {
	// Next returns the next instruction, or false if the end of the program
	// has been reached.
	Next() (Instruction, bool)

	// JumpCount moves a relative count of instructions forward or back in the
	// program and returns whether the jump was successful.
	//
	// A jump may be unsuccessful if the number of instructions to jump exceeds
	// the beginning or end of the program.
	JumpCount(count int) bool
}

type exprProgram struct {
	expression      *exprpb.Expr
	instructions    []Instruction
	metadata        Metadata
	revInstructions map[int64]int
	shortCircuit    bool
}

// NewCheckedProgram creates a Program from a checked CEL expression.
func NewCheckedProgram(c *exprpb.CheckedExpr) Program {
	// TODO: take advantage of the type-check information.
	return NewProgram(c.Expr, c.SourceInfo)
}

// NewProgram creates a Program from a CEL expression and source information.
func NewProgram(expression *exprpb.Expr,
	info *exprpb.SourceInfo) Program {
	revInstructions := make(map[int64]int)
	return &exprProgram{
		expression:      expression,
		revInstructions: revInstructions,
		metadata:        newExprMetadata(info),
		shortCircuit:    true,
	}
}

// NewExhaustiveProgram creates a Program from a CEL expression and source
// information which force evaluating all branches of the expression.
func NewExhaustiveProgram(expression *exprpb.Expr,
	// TODO: also disable short circuit in comprehensions.
	info *exprpb.SourceInfo) Program {
	revInstructions := make(map[int64]int)
	return &exprProgram{
		expression:      expression,
		revInstructions: revInstructions,
		metadata:        newExprMetadata(info),
		shortCircuit:    false,
	}
}

func (p *exprProgram) Begin() InstructionStepper {
	if p.instructions == nil {
		panic("the Begin() method was called before program Init()")
	}
	return &exprStepper{p, 0}
}

func (p *exprProgram) GetInstruction(runtimeID int64) Instruction {
	return p.instructions[p.revInstructions[runtimeID]]
}

func (p *exprProgram) Init(dispatcher Dispatcher, state MutableEvalState) {
	if p.instructions == nil {
		p.instructions = WalkExpr(p.expression, p.metadata, dispatcher, state, p.shortCircuit)
		for i, inst := range p.instructions {
			p.revInstructions[inst.GetID()] = i
		}
	}
}

func (p *exprProgram) MaxInstructionID() int64 {
	// The max instruction id is the highest expression id in the program,
	// plus the count of the internal variables allocated for comprehensions.
	//
	// A comprehension allocates an id for each of the following:
	// - iterator
	// - hasNext() result
	// - iterVar
	//
	// The maxID is thus, the max input id + comprehension count * 3
	return maxID(p.expression) + comprehensionCount(p.expression)*3
}

func (p *exprProgram) Metadata() Metadata {
	return p.metadata
}

func (p *exprProgram) String() string {
	instStrs := make([]string, len(p.instructions), len(p.instructions))
	for i, inst := range p.instructions {
		instStrs[i] = fmt.Sprintf("%d: %v", i, inst)
	}
	return strings.Join(instStrs, "\n")
}

// exprStepper keeps a cursor pointed at the next instruction to execute
// in the program.
type exprStepper struct {
	program     *exprProgram
	instruction int
}

func (s *exprStepper) Next() (Instruction, bool) {
	if s.instruction < len(s.program.instructions) {
		inst := s.instruction
		s.instruction++
		return s.program.instructions[inst], true
	}
	return nil, false
}

func (s *exprStepper) JumpCount(count int) bool {
	// Adjust for the cursor already having been moved.
	offset := count - 1
	candidate := s.instruction + offset
	if candidate >= 0 && candidate < len(s.program.instructions) {
		s.instruction = candidate
		return true
	}
	return false
}

// The exprMetadata type provides helper functions for retrieving source
// locations in a human readable manner based on the data contained within
// the expr.SourceInfo message.
type exprMetadata struct {
	info *exprpb.SourceInfo
}

func newExprMetadata(info *exprpb.SourceInfo) Metadata {
	return &exprMetadata{info: info}
}

func (m *exprMetadata) IDLocation(exprID int64) (common.Location, bool) {
	if exprOffset, found := m.IDOffset(exprID); found {
		var index = 0
		var lineIndex = 0
		var lineOffset int32
		for index, lineOffset = range m.info.LineOffsets {
			if lineOffset > exprOffset {
				break
			}
			lineIndex = index
		}
		line := lineIndex + 1
		column := exprOffset - lineOffset
		return common.NewLocation(line, int(column)), true
	}
	return nil, false
}

func (m *exprMetadata) IDOffset(exprID int64) (int32, bool) {
	position, found := m.info.Positions[exprID]
	return position, found
}
