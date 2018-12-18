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

	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

const (
	// Constant used to generate symbols during AST walking.
	genSymFormat = "_sym_@%d"
)

// WalkExpr produces a set of Instruction values from a CEL expression.
//
// WalkExpr does a post-order traversal of a CEL syntax AST, which means
// expressions are evaluated in a bottom-up fashion just as they would be in
// a recursive execution pattern.
func WalkExpr(expression *exprpb.Expr,
	metadata Metadata,
	dispatcher Dispatcher,
	state MutableEvalState,
	shortCircuit bool) []Instruction {
	nextID := maxID(expression)
	walker := &astWalker{
		dispatcher:   dispatcher,
		genSymID:     nextID,
		genExprID:    nextID,
		metadata:     metadata,
		scope:        newScope(),
		state:        state,
		shortCircuit: shortCircuit}
	return walker.walk(expression)
}

// astWalker implementation of the AST walking logic.
type astWalker struct {
	dispatcher   Dispatcher
	genExprID    int64
	genSymID     int64
	metadata     Metadata
	scope        *blockScope
	state        MutableEvalState
	shortCircuit bool
}

func (w *astWalker) walk(node *exprpb.Expr) []Instruction {
	switch node.ExprKind.(type) {
	case *exprpb.Expr_CallExpr:
		return w.walkCall(node)
	case *exprpb.Expr_IdentExpr:
		return w.walkIdent(node)
	case *exprpb.Expr_SelectExpr:
		return w.walkSelect(node)
	case *exprpb.Expr_ConstExpr:
		w.walkLiteral(node)
		return []Instruction{}
	case *exprpb.Expr_ListExpr:
		return w.walkList(node)
	case *exprpb.Expr_StructExpr:
		return w.walkStruct(node)
	case *exprpb.Expr_ComprehensionExpr:
		return w.walkComprehension(node)
	}
	return []Instruction{}
}

func (w *astWalker) walkLiteral(node *exprpb.Expr) {
	literal := node.GetConstExpr()
	var value ref.Value
	switch literal.ConstantKind.(type) {
	case *exprpb.Constant_BoolValue:
		value = types.Bool(literal.GetBoolValue())
	case *exprpb.Constant_BytesValue:
		value = types.Bytes(literal.GetBytesValue())
	case *exprpb.Constant_DoubleValue:
		value = types.Double(literal.GetDoubleValue())
	case *exprpb.Constant_Int64Value:
		value = types.Int(literal.GetInt64Value())
	case *exprpb.Constant_NullValue:
		value = types.Null(literal.GetNullValue())
	case *exprpb.Constant_StringValue:
		value = types.String(literal.GetStringValue())
	case *exprpb.Constant_Uint64Value:
		value = types.Uint(literal.GetUint64Value())
	}
	w.state.SetValue(node.Id, value)
}

func (w *astWalker) walkIdent(node *exprpb.Expr) []Instruction {
	identName := node.GetIdentExpr().Name
	if _, found := w.scope.ref(identName); !found {
		ident := NewIdent(node.Id, identName)
		w.scope.setRef(identName, node.Id)
		return []Instruction{ident}
	}
	return []Instruction{}
}

func (w *astWalker) walkSelect(node *exprpb.Expr) []Instruction {
	sel := node.GetSelectExpr()
	operandID := w.getID(sel.Operand)
	return append(
		w.walk(sel.Operand),
		NewSelect(node.Id, operandID, sel.Field))
}

func (w *astWalker) walkCall(node *exprpb.Expr) []Instruction {
	call := node.GetCallExpr()
	function := call.Function
	argGroups, argGroupLens, argIDs := w.walkCallArgs(call)
	argCount := len(argIDs)

	// Compute the instruction set, making sure to special case the behavior of
	// logical and, logical or, and conditional operators.
	var instructions []Instruction
	switch function {
	case operators.LogicalAnd, operators.LogicalOr:
		// Compute the left-hand side with a jump if the value can be used to
		// short-circuit the expression.
		//
		// Instruction layout:
		// 0: lhs expr
		// 1: jump to <END> on true (||), false (&&)
		// 2: rhs expr
		// 3: <END> logical-op(lhs, rhs)
		var instructionCount = argCount - 1
		for _, argGroupLen := range argGroupLens {
			instructionCount += argGroupLen
		}
		var evalCount = 0
		// Logical operators may have more than two arg groups in the future.
		// e.g, and(a, b, c) === a && b && c.
		// Ensure the groups are appropriately laid-out in memory.
		for i, argGroup := range argGroups {
			evalCount += argGroupLens[i]
			instructions = append(instructions, argGroup...)
			if i != argCount-1 && w.shortCircuit {
				instructions = append(instructions,
					NewJump(argIDs[i], instructionCount-evalCount,
						jumpIfEqual(argIDs[i], types.Bool(function == operators.LogicalOr))))
				evalCount++
			}
		}
		return append(instructions, NewCall(node.Id, call.Function, argIDs))

	case operators.Conditional:
		// Compute the conditional jump, with two jumps, one for false,
		// and one for true
		//
		// Instruction layout:
		// 0: condition
		// 1: jump to <END> on undefined/error
		// 2: jump to <ELSE> on false
		// 3: <IF> expr
		// 4: jump to <END>
		// 5: <ELSE> expr
		// 6: <END> ternary
		conditionID, condition := argIDs[0], argGroups[0]
		trueID, trueVal := argIDs[1], argGroups[1]
		falseVal := argGroups[2]

		// 0: condition
		instructions = append(instructions, condition...)
		// 1: jump to <END> on undefined/error
		if w.shortCircuit {
			instructions = append(instructions,
				NewJump(conditionID, len(trueVal)+len(falseVal)+3,
					jumpIfUnknownOrError(conditionID)))
		}
		// 2: jump to <ELSE> on false.
		if w.shortCircuit {
			instructions = append(instructions,
				NewJump(conditionID, len(trueVal)+2,
					jumpIfEqual(conditionID, types.False)))
		}
		// 3: <IF> expr
		instructions = append(instructions, trueVal...)
		// 4: jump to <END>
		if w.shortCircuit {
			instructions = append(instructions,
				NewJump(trueID, len(falseVal)+1, jumpAlways))
		}
		// 5: <ELSE> expr
		instructions = append(instructions, falseVal...)
		// 6: <END> ternary
		return append(instructions, NewCall(node.Id, call.Function, argIDs))

	default:
		for _, argGroup := range argGroups {
			instructions = append(instructions, argGroup...)
		}
		return append(instructions, NewCall(node.Id, call.Function, argIDs))
	}
}

func (w *astWalker) walkList(node *exprpb.Expr) []Instruction {
	listExpr := node.GetListExpr()
	var elementIDs []int64
	var elementSteps []Instruction
	for _, elem := range listExpr.GetElements() {
		elementIDs = append(elementIDs, w.getID(elem))
		elementSteps = append(elementSteps, w.walk(elem)...)
	}
	return append(elementSteps, NewList(node.Id, elementIDs))
}

func (w *astWalker) walkStruct(node *exprpb.Expr) []Instruction {
	structExpr := node.GetStructExpr()
	keyValues := make(map[int64]int64)
	fieldValues := make(map[string]int64)
	var entrySteps []Instruction
	for _, entry := range structExpr.GetEntries() {
		valueID := w.getID(entry.GetValue())
		switch entry.KeyKind.(type) {
		case *exprpb.Expr_CreateStruct_Entry_FieldKey:
			fieldValues[entry.GetFieldKey()] = valueID
		case *exprpb.Expr_CreateStruct_Entry_MapKey:
			keyValues[w.getID(entry.GetMapKey())] = valueID
			entrySteps = append(entrySteps, w.walk(entry.GetMapKey())...)
		}
		entrySteps = append(entrySteps, w.walk(entry.GetValue())...)
	}
	if len(structExpr.MessageName) == 0 {
		return append(entrySteps, NewMap(node.Id, keyValues))
	}
	return append(entrySteps,
		NewObject(node.Id, structExpr.MessageName, fieldValues))
}

func (w *astWalker) walkComprehension(node *exprpb.Expr) []Instruction {
	// Serializing a comprehension into a linear set of executable steps is one
	// of the more complex tasks in AST walking. The challenge being loop
	// termination when errors or unknown values are encountered outside
	// of the accumulation steps.

	// The following example indicate sthe set of steps for the 'all' macro
	//
	// Expr: list.all(x, x < 10)
	//
	// Instruction layout:
	// 0: list                            # iter-range
	// 1: push-scope accu, iterVar, it
	// 2: accu = true                     # init
	// 3: it = list.iterator()            # iterator()
	// <LOOP>
	// 4: hasNext = it.hasNext()          # hasNext()?
	// 5: jump <END> if !hasNext
	// 6: iterVar = it.next()             # it.next()
	// 7: loop = not_strictly_false(accu) # loopCondition
	// 8: jump <END> if !loop
	// 9: accu = accu && iterVar < 10 # loopStep
	// 10: jump <LOOP>
	// <END>
	// 11: result = accu                   # result
	// 12: comp = result
	// 13: pop-scope
	comprehensionExpr := node.GetComprehensionExpr()
	comprehensionRange := comprehensionExpr.GetIterRange()
	comprehensionAccu := comprehensionExpr.GetAccuInit()
	comprehensionLoop := comprehensionExpr.GetLoopCondition()
	comprehensionStep := comprehensionExpr.GetLoopStep()
	result := comprehensionExpr.GetResult()

	// iter-range
	rangeSteps := w.walk(comprehensionRange)

	// Push Module with the accumulator, iter var, and iterator
	iteratorID := w.nextExprID()
	iterNextID := w.nextExprID()
	iterSymID := w.nextSymID()
	accuID := w.getID(comprehensionAccu)
	loopID := w.getID(comprehensionLoop)
	stepID := w.getID(comprehensionStep)
	pushScopeStep := NewPushScope(
		node.GetId(),
		map[string]int64{
			comprehensionExpr.AccuVar: accuID,
			comprehensionExpr.IterVar: iterNextID,
			iterSymID:                 iteratorID})
	currScope := newScope()
	currScope.setRef(comprehensionExpr.AccuVar, accuID)
	currScope.setRef(comprehensionExpr.IterVar, iterNextID)
	currScope.setRef(iterSymID, iteratorID)
	w.pushScope(currScope)
	// accu-init
	accuInitSteps := w.walk(comprehensionAccu)

	// iter-init
	iterInitStep :=
		NewCall(iteratorID, overloads.Iterator, []int64{w.getID(comprehensionRange)})

	// <LOOP>
	// Loop instruction breakdown
	// 1:                       <LOOP> it.hasNext()
	// 2:                       jmpif false, <END>
	// 3:                       x = it.next()
	// 3+len(cond):             <cond>
	// 4+len(cond):             jmpif false, <END>
	// 4+len(cond)+len(step):   <step>
	// 5+len(cond)+len(step):   mov step, accu
	// 6+len(cond)+len(step):   jmp LOOP
	// 7+len(cond)+len(step)    <result>
	// <END>
	loopConditionSteps := w.walk(comprehensionLoop)
	loopBodySteps := w.walk(comprehensionStep)
	// Add 4 steps to the loopBody to account for:
	// - fetching the next item from the range
	// - conditional jump to terminate the loop based on __result__
	// - update of the __result__ based on the loop step.
	// - jump to repeat the loop
	loopBodyInstructionCount := 4 + len(loopBodySteps)
	// Add 1 step to account for the jump computation based on the condition.
	loopInstructionCount := 1 + len(loopConditionSteps) + loopBodyInstructionCount

	// iter-hasNext
	iterHasNextID := w.nextExprID()
	iterHasNextStep :=
		NewCall(iterHasNextID, overloads.HasNext, []int64{iteratorID})
	// jump <END> if !it.hasNext()
	jumpIterEndStep :=
		NewJump(iterHasNextID, loopInstructionCount, breakIfEnd(iterHasNextID))
	// eval x = it.next()
	// eval <cond>
	// jump <END> if condition false
	jumpConditionFalseStep :=
		NewJump(loopID, loopBodyInstructionCount, jumpIfEqual(loopID, types.False))

	// iter-next
	nextIterVarStep := NewCall(iterNextID, overloads.Next, []int64{iteratorID})
	// assign the loop-step to the accu var
	accuUpdateStep := NewMov(stepID, accuID)
	// jump <LOOP>
	jumpCondStep := NewJump(stepID, -loopInstructionCount, jumpAlways)

	// <END> result
	resultSteps := w.walk(result)
	compResultUpdateStep := NewMov(w.getID(result), w.getID(node))
	popScopeStep := NewPopScope(w.getID(node))
	w.popScope()

	var instructions []Instruction
	instructions = append(instructions, rangeSteps...)
	instructions = append(instructions, pushScopeStep)
	instructions = append(instructions, accuInitSteps...)
	instructions = append(instructions, iterInitStep, iterHasNextStep, jumpIterEndStep)
	instructions = append(instructions, loopConditionSteps...)
	instructions = append(instructions, jumpConditionFalseStep, nextIterVarStep)
	instructions = append(instructions, loopBodySteps...)
	instructions = append(instructions, accuUpdateStep, jumpCondStep)
	instructions = append(instructions, resultSteps...)
	instructions = append(instructions, compResultUpdateStep, popScopeStep)
	return instructions
}

func (w *astWalker) walkCallArgs(call *exprpb.Expr_Call) (
	argGroups [][]Instruction, argGroupLens []int, argIDs []int64) {
	args := getArgs(call)
	argCount := len(args)
	argGroups = make([][]Instruction, argCount)
	argGroupLens = make([]int, argCount)
	argIDs = make([]int64, argCount)
	for i, arg := range getArgs(call) {
		argIDs[i] = w.getID(arg)
		argGroups[i] = w.walk(arg)
		argGroupLens[i] = len(argGroups[i])
	}
	return // named outputs.
}

// Helper functions.

// getArgs returns a unified set of call args for both global and receiver
// style calls.
func getArgs(call *exprpb.Expr_Call) []*exprpb.Expr {
	var argSet []*exprpb.Expr
	if call.Target != nil {
		argSet = append(argSet, call.Target)
	}
	if call.GetArgs() != nil {
		argSet = append(argSet, call.GetArgs()...)
	}
	return argSet
}

// nextSymID generates an expression-unique identifier name for identifiers
// that need to be produced programmatically.
func (w *astWalker) nextSymID() string {
	nextID := w.genSymID
	w.genSymID++
	return fmt.Sprintf(genSymFormat, nextID)
}

// nextExprID generates expression ids when they are necessary for tracking
// evaluation state, but not captured as part of the AST.
func (w *astWalker) nextExprID() int64 {
	nextID := w.genExprID
	w.genExprID++
	return nextID
}

// pushScope moves a new scope for expression id resolution onto the stack,
// so that the same identifier name may be used in nested contexts (such as
// nested comprehensions), but that the expression ids are kept unique per
// scope.
func (w *astWalker) pushScope(scope *blockScope) {
	scope.parent = w.scope
	w.scope = scope
}

// popScope restores the identifier to expression id mapping defined in the
// prior scope.
func (w *astWalker) popScope() {
	w.scope = w.scope.parent
}

// getID returns the expression id associated with a given identifier if one
// has been set within the current scope, else the expression id.
func (w *astWalker) getID(expr *exprpb.Expr) int64 {
	id := expr.GetId()
	if ident := expr.GetIdentExpr(); ident != nil {
		if altID, found := w.scope.ref(ident.Name); found {
			w.state.SetRuntimeExpressionID(id, altID)
			return altID
		}
	}
	return id
}

// blockScope tracks identifier references within a scope and ensures that for
// all possible references to the same identifier, the same expression id is
// used within generated Instruction values.
type blockScope struct {
	parent     *blockScope
	references map[string]int64
}

func newScope() *blockScope {
	return &blockScope{references: make(map[string]int64)}
}

func (b *blockScope) ref(ident string) (int64, bool) {
	if inst, found := b.references[ident]; found {
		return inst, found
	} else if b.parent != nil {
		return b.parent.ref(ident)
	}
	return 0, false
}

func (b *blockScope) setRef(ident string, exprID int64) {
	b.references[ident] = exprID
}

func jumpIfUnknownOrError(exprID int64) func(EvalState) bool {
	return func(s EvalState) bool {
		if val, found := s.Value(exprID); found {
			return types.IsUnknown(val) || types.IsError(val)
		}
		return false
	}
}

func breakIfEnd(conditionID int64) func(EvalState) bool {
	return func(s EvalState) bool {
		if val, found := s.Value(conditionID); found {
			return val == types.False ||
				types.IsUnknown(val) ||
				types.IsError(val)
		}
		return true
	}
}

func jumpIfEqual(exprID int64, value ref.Value) func(EvalState) bool {
	return func(s EvalState) bool {
		if val, found := s.Value(exprID); found {
			if types.IsBool(val) {
				return bool(val.Equal(value).(types.Bool))
			}
		}
		return false
	}
}

func jumpAlways(_ EvalState) bool {
	return true
}

func comprehensionCount(nodes ...*exprpb.Expr) int64 {
	if nodes == nil || len(nodes) == 0 {
		return 0
	}
	count := int64(0)
	for _, node := range nodes {
		if node == nil {
			continue
		}
		switch node.ExprKind.(type) {
		case *exprpb.Expr_SelectExpr:
			count += comprehensionCount(node.GetSelectExpr().GetOperand())
		case *exprpb.Expr_CallExpr:
			call := node.GetCallExpr()
			count += comprehensionCount(call.GetTarget()) + comprehensionCount(call.GetArgs()...)
		case *exprpb.Expr_ListExpr:
			count += comprehensionCount(node.GetListExpr().GetElements()...)
		case *exprpb.Expr_StructExpr:
			for _, entry := range node.GetStructExpr().GetEntries() {
				count += comprehensionCount(entry.GetMapKey()) +
					comprehensionCount(entry.GetValue())
			}
		case *exprpb.Expr_ComprehensionExpr:
			compre := node.GetComprehensionExpr()
			count++
			count += comprehensionCount(compre.IterRange) +
				comprehensionCount(compre.AccuInit) +
				comprehensionCount(compre.LoopCondition) +
				comprehensionCount(compre.LoopStep) +
				comprehensionCount(compre.Result)
		}
	}
	return count
}

func maxID(node *exprpb.Expr) int64 {
	if node == nil {
		return 0
	}
	currID := node.Id
	switch node.ExprKind.(type) {
	case *exprpb.Expr_SelectExpr:
		return maxInt(currID, maxID(node.GetSelectExpr().Operand))
	case *exprpb.Expr_CallExpr:
		call := node.GetCallExpr()
		currID = maxInt(currID, maxID(call.Target))
		for _, arg := range call.Args {
			currID = maxInt(currID, maxID(arg))
		}
		return currID
	case *exprpb.Expr_ListExpr:
		list := node.GetListExpr()
		for _, elem := range list.Elements {
			currID = maxInt(currID, maxID(elem))
		}
		return currID
	case *exprpb.Expr_StructExpr:
		str := node.GetStructExpr()
		for _, entry := range str.Entries {
			currID = maxInt(currID, entry.Id, maxID(entry.GetMapKey()), maxID(entry.Value))
		}
		return currID
	case *exprpb.Expr_ComprehensionExpr:
		compre := node.GetComprehensionExpr()
		return maxInt(currID,
			maxID(compre.IterRange),
			maxID(compre.AccuInit),
			maxID(compre.LoopCondition),
			maxID(compre.LoopStep),
			maxID(compre.Result))
	default:
		return currID
	}
}

func maxInt(vals ...int64) int64 {
	var result int64
	for _, val := range vals {
		if val > result {
			result = val
		}
	}
	return result
}
