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
	"github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type astPruner struct {
	expr  *exprpb.Expr
	state EvalState
}

// TODO Consider having a separate walk of the AST that finds common
// subexpressions. This can be called before or after constant folding to find
// common subexpressions.

// PruneAst prunes the given AST based on the given EvalState and generates a new AST.
// Given AST is copied on write and a new AST is returned.
// Couple of typical use cases this interface would be:
//
// A)
// 1) Evaluate expr with some unknowns,
// 2) If result is unknown:
//   a) PruneAst
//   b) Goto 1
// Functional call results which are known would be effectively cached across
// iterations.
//
// B)
// 1) Compile the expression (maybe via a service and maybe after checking a
//    compiled expression does not exists in local cache)
// 2) Prepare the environment and the interpreter. Activation might be empty.
// 3) Eval the expression. This might return unknown or error or a concrete
//    value.
// 4) PruneAst
// 4) Maybe cache the expression
// This is effectively constant folding the expression. How the environment is
// prepared in step 2 is flexible. For example, If the caller caches the
// compiled and constant folded expressions, but is not willing to constant
// fold(and thus cache results of) some external calls, then they can prepare
// the overloads accordingly.
func PruneAst(expr *exprpb.Expr, state EvalState) *exprpb.Expr {
	pruner := &astPruner{
		expr:  expr,
		state: state}
	newExpr, _ := pruner.prune(expr)
	return newExpr
}

func (p *astPruner) createLiteral(node *exprpb.Expr, val *exprpb.Constant) *exprpb.Expr {
	newExpr := *node
	newExpr.ExprKind = &exprpb.Expr_ConstExpr{ConstExpr: val}
	return &newExpr
}

func (p *astPruner) maybePruneAndOr(node *exprpb.Expr) (*exprpb.Expr, bool) {
	if !p.existsWithUnknownValue(node.GetId()) {
		return nil, false
	}

	call := node.GetCallExpr()

	// We know result is unknown, so we have at least one unknown arg
	// and if one side is a known value, we know we can ignore it.
	if p.existsWithKnownValue(call.Args[0].GetId()) {
		return call.Args[1], true
	}

	if p.existsWithKnownValue(call.Args[1].GetId()) {
		return call.Args[0], true
	}
	return nil, false
}

func (p *astPruner) maybePruneConditional(node *exprpb.Expr) (*exprpb.Expr, bool) {
	if !p.existsWithUnknownValue(node.GetId()) {
		return nil, false
	}

	call := node.GetCallExpr()
	condVal, condValueExists := p.value(call.Args[0].GetId())
	if !condValueExists || types.IsUnknownOrError(condVal) {
		return nil, false
	}

	if condVal.Value().(bool) {
		return call.Args[1], true
	}
	return call.Args[2], true
}

func (p *astPruner) maybePruneFunction(node *exprpb.Expr) (*exprpb.Expr, bool) {
	call := node.GetCallExpr()
	if call.Function == operators.LogicalOr || call.Function == operators.LogicalAnd {
		return p.maybePruneAndOr(node)
	}
	if call.Function == operators.Conditional {
		return p.maybePruneConditional(node)
	}

	return nil, false
}

func (p *astPruner) prune(node *exprpb.Expr) (*exprpb.Expr, bool) {
	if node == nil {
		return node, false
	}
	if val, valueExists := p.value(node.GetId()); valueExists && !types.IsUnknownOrError(val) {

		// TODO if we have a list or struct, create a list/struct
		// expression. This is useful especially if these expressions
		// are result of a function call.

		switch val.Type() {
		case types.BoolType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_BoolValue{BoolValue: val.Value().(bool)}}), true
		case types.IntType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_Int64Value{Int64Value: val.Value().(int64)}}), true
		case types.UintType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_Uint64Value{Uint64Value: val.Value().(uint64)}}), true
		case types.StringType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_StringValue{StringValue: val.Value().(string)}}), true
		case types.DoubleType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_DoubleValue{DoubleValue: val.Value().(float64)}}), true
		case types.BytesType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_BytesValue{BytesValue: val.Value().([]byte)}}), true
		case types.NullType:
			return p.createLiteral(node,
				&exprpb.Constant{ConstantKind: &exprpb.Constant_NullValue{NullValue: val.Value().(structpb.NullValue)}}), true
		}
	}

	// We have either an unknown/error value, or something we dont want to
	// transform, or expression was not evaluated. If possible, drill down
	// more.

	switch node.ExprKind.(type) {
	case *exprpb.Expr_SelectExpr:
		if operand, pruned := p.prune(node.GetSelectExpr().Operand); pruned {
			newExpr := *node
			newSelect := *newExpr.GetSelectExpr()
			newSelect.Operand = operand
			newExpr.GetExprKind().(*exprpb.Expr_SelectExpr).SelectExpr = &newSelect
			return &newExpr, true
		}
	case *exprpb.Expr_CallExpr:
		if newExpr, pruned := p.maybePruneFunction(node); pruned {
			newExpr, _ = p.prune(newExpr)
			return newExpr, true
		}
		newCall := *node.GetCallExpr()
		var prunedCall bool
		var prunedArg bool
		for i, arg := range node.GetCallExpr().Args {
			if newCall.Args[i], prunedArg = p.prune(arg); prunedArg {
				prunedCall = true
			}
		}
		if newTarget, prunedTarget := p.prune(node.GetCallExpr().Target); prunedTarget {
			prunedCall = true
			newCall.Target = newTarget
		}
		if prunedCall {
			newExpr := *node
			newExpr.GetExprKind().(*exprpb.Expr_CallExpr).CallExpr = &newCall
			return &newExpr, true
		}
	case *exprpb.Expr_ListExpr:
		newList := *node.GetListExpr()
		var prunedList bool
		var prunedElem bool
		for i, elem := range node.GetListExpr().Elements {
			if newList.Elements[i], prunedElem = p.prune(elem); prunedElem {
				prunedList = true
			}
		}
		if prunedList {
			newExpr := *node
			newExpr.GetExprKind().(*exprpb.Expr_ListExpr).ListExpr = &newList
			return &newExpr, true
		}
	case *exprpb.Expr_StructExpr:
		newStruct := *node.GetStructExpr()
		var prunedStruct bool
		var prunedEntry bool
		for i, entry := range node.GetStructExpr().Entries {
			newEntry := *entry
			if newKey, pruned := p.prune(entry.GetMapKey()); pruned {
				prunedEntry = true
				newEntry.GetKeyKind().(*exprpb.Expr_CreateStruct_Entry_MapKey).MapKey = newKey
			}
			if newValue, pruned := p.prune(entry.Value); pruned {
				prunedEntry = true
				newEntry.Value = newValue
			}
			if prunedEntry {
				prunedStruct = true
				newStruct.Entries[i] = &newEntry
			}
		}
		if prunedStruct {
			newExpr := *node
			newExpr.GetExprKind().(*exprpb.Expr_StructExpr).StructExpr = &newStruct
			return &newExpr, true
		}
	case *exprpb.Expr_ComprehensionExpr:
		if newIterRange, pruned := p.prune(node.GetComprehensionExpr().IterRange); pruned {
			newExpr := *node
			newCompre := *newExpr.GetComprehensionExpr()
			newCompre.IterRange = newIterRange
			newExpr.GetExprKind().(*exprpb.Expr_ComprehensionExpr).ComprehensionExpr = &newCompre
			return &newExpr, true
		}
	}
	return node, false
}

func (p *astPruner) value(id int64) (ref.Value, bool) {
	val, found := p.state.Value(p.state.GetRuntimeExpressionID(id))
	return val, (found && val != nil)
}

func (p *astPruner) existsWithUnknownValue(id int64) bool {
	val, valueExists := p.value(id)
	return valueExists && types.IsUnknown(val)
}

func (p *astPruner) existsWithKnownValue(id int64) bool {
	val, valueExists := p.value(id)
	return valueExists && !types.IsUnknown(val)
}
