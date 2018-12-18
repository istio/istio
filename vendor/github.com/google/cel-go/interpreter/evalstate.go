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
	"github.com/google/cel-go/common/types/ref"
)

// EvalState tracks the values associated with expression ids during execution.
type EvalState interface {
	// GetRuntimeExpressionID returns the runtime id corresponding to the
	// expression id from the AST.
	GetRuntimeExpressionID(exprID int64) int64

	// OnlyValue returns the value in the eval state, if only one exists.
	OnlyValue() (ref.Value, bool)

	// Value of the given expression id, false if not found.
	Value(int64) (ref.Value, bool)
}

// MutableEvalState permits the mutation of evaluation state for a given
// expression id.
type MutableEvalState interface {
	EvalState

	// SetRuntimeExpressionID sets the runtime id for the given expr id.
	SetRuntimeExpressionID(exprID int64, runtimeID int64)

	// SetValue associates an expression id with a value.
	SetValue(int64, ref.Value)
}

// NewEvalState returns a MutableEvalState.
func NewEvalState(instructionCount int64) MutableEvalState {
	return &defaultEvalState{exprCount: instructionCount,
		exprValues: make([]ref.Value, instructionCount, instructionCount),
		exprIDMap:  make(map[int64]int64)}
}

type defaultEvalState struct {
	exprCount  int64
	exprValues []ref.Value
	exprIDMap  map[int64]int64
}

func (s *defaultEvalState) GetRuntimeExpressionID(exprID int64) int64 {
	if val, ok := s.exprIDMap[exprID]; ok {
		return val
	}
	return exprID
}

func (s *defaultEvalState) OnlyValue() (ref.Value, bool) {
	var result ref.Value
	i := 0
	for _, val := range s.exprValues {
		if val != nil {
			result = val
			i++
		}
	}
	if i == 1 {
		return result, true
	}
	return nil, false
}

func (s *defaultEvalState) SetRuntimeExpressionID(exprID int64, runtimeID int64) {
	s.exprIDMap[exprID] = runtimeID
}

func (s *defaultEvalState) SetValue(exprID int64, value ref.Value) {
	s.exprValues[exprID] = value
}

func (s *defaultEvalState) Value(exprID int64) (ref.Value, bool) {
	// TODO: The eval state assumes a dense progrma expression id space. While
	// this is true of how the cel-go parser generates identifiers, it may not
	// be true for all implementations or for the long term. Replace the use of
	// parse-time generated expression ids with a dense runtiem identifier.
	if exprID >= 0 && exprID < s.exprCount {
		return s.exprValues[exprID], true
	}
	return nil, false
}
