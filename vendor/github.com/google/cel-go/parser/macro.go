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

package parser

import (
	"fmt"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/operators"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// TODO: Consider moving macros to common.

// Macros type alias for a collection of Macros.
type Macros []Macro

// Macro type which declares the function name and arg count expected for the
// macro, as well as a macro expansion function.
type Macro struct {
	name          string
	instanceStyle bool
	args          int
	expander      func(*parserHelper, interface{}, *exprpb.Expr, []*exprpb.Expr) (*exprpb.Expr, *common.Error)
}

// AllMacros includes the list of all spec-supported macros.
var AllMacros = []Macro{
	// The macro "has(m.f)" which tests the presence of a field, avoiding the need to specify
	// the field as a string.
	{
		name:          operators.Has,
		instanceStyle: false,
		args:          1,
		expander:      makeHas,
	},
	// The macro "range.all(var, predicate)", which is true if for all elements in range the  predicate holds.
	{
		name:          operators.All,
		instanceStyle: true,
		args:          2,
		expander:      makeAll,
	},
	// The macro "range.exists(var, predicate)", which is true if for at least one element in
	// range the predicate holds.
	{
		name:          operators.Exists,
		instanceStyle: true,
		args:          2,
		expander:      makeExists,
	},
	// The macro "range.exists_one(var, predicate)", which is true if for exactly one element
	// in range the predicate holds.
	{
		name:          operators.ExistsOne,
		instanceStyle: true,
		args:          2,
		expander:      makeExistsOne,
	},
	// The macro "range.map(var, function)", applies the function to the vars in the range.
	{
		name:          operators.Map,
		instanceStyle: true,
		args:          2,
		expander:      makeMap,
	},
	// The macro "range.map(var, predicate, function)", applies the function to the vars in
	// the range for which the predicate holds true. The other variables are filtered out.
	{
		name:          operators.Map,
		instanceStyle: true,
		args:          3,
		expander:      makeMap,
	},
	// The macro "range.filter(var, predicate)", filters out the variables for which the
	// predicate is false.
	{
		name:          operators.Filter,
		instanceStyle: true,
		args:          2,
		expander:      makeFilter,
	},
}

// NoMacros list.
var NoMacros = []Macro{}

// GetName returns the macro's name (i.e. the function whose syntax it mimics).
func (m *Macro) GetName() string {
	return m.name
}

// GetArgCount returns the number of arguments the macro expects.
func (m *Macro) GetArgCount() int {
	return m.args
}

// GetIsInstanceStyle returns whether the macro is "instance" (reciever) style.
func (m *Macro) GetIsInstanceStyle() bool {
	return m.instanceStyle
}

func makeHas(p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	if s, ok := args[0].ExprKind.(*exprpb.Expr_SelectExpr); ok {
		return p.newPresenceTest(ctx, s.SelectExpr.Operand, s.SelectExpr.Field), nil
	}
	return nil, &common.Error{Message: "invalid argument to has() macro"}
}

const accumulatorName = "__result__"

type quantifierKind int

const (
	quantifierAll quantifierKind = iota
	quantifierExists
	quantifierExistsOne
)

func makeAll(p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	return makeQuantifier(quantifierAll, p, ctx, target, args)
}

func makeExists(p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	return makeQuantifier(quantifierExists, p, ctx, target, args)
}

func makeExistsOne(p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	return makeQuantifier(quantifierExistsOne, p, ctx, target, args)
}

func makeQuantifier(kind quantifierKind, p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	v, found := extractIdent(args[0])
	if !found {
		offset := p.positions[args[0].Id]
		location, _ := p.source.OffsetLocation(offset)
		return nil, &common.Error{
			Message:  "argument must be a simple name",
			Location: location}
	}
	accuIdent := func() *exprpb.Expr {
		return p.newIdent(ctx, accumulatorName)
	}

	var init *exprpb.Expr
	var condition *exprpb.Expr
	var step *exprpb.Expr
	var result *exprpb.Expr
	switch kind {
	case quantifierAll:
		init = p.newLiteralBool(ctx, true)
		condition = p.newGlobalCall(ctx, operators.NotStrictlyFalse, accuIdent())
		step = p.newGlobalCall(ctx, operators.LogicalAnd, accuIdent(), args[1])
		result = accuIdent()
	case quantifierExists:
		init = p.newLiteralBool(ctx, false)
		condition = p.newGlobalCall(ctx,
			operators.NotStrictlyFalse,
			p.newGlobalCall(ctx, operators.LogicalNot, accuIdent()))
		step = p.newGlobalCall(ctx, operators.LogicalOr, accuIdent(), args[1])
		result = accuIdent()
	case quantifierExistsOne:
		// TODO: make consistent with the CEL semantics.
		zeroExpr := p.newLiteralInt(ctx, 0)
		oneExpr := p.newLiteralInt(ctx, 1)
		init = zeroExpr
		condition = p.newGlobalCall(ctx, operators.LessEquals, accuIdent(), oneExpr)
		step = p.newGlobalCall(ctx, operators.Conditional, args[1],
			p.newGlobalCall(ctx, operators.Add, accuIdent(), oneExpr), accuIdent())
		result = p.newGlobalCall(ctx, operators.Equals, accuIdent(), oneExpr)
	default:
		return nil, &common.Error{Message: fmt.Sprintf("unrecognized quantifier '%v'", kind)}
	}
	return p.newComprehension(ctx, v, target, accumulatorName, init, condition, step, result), nil
}

func makeMap(p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	v, found := extractIdent(args[0])
	if !found {
		return nil, &common.Error{Message: "argument is not an identifier"}
	}

	var fn *exprpb.Expr
	var filter *exprpb.Expr

	if len(args) == 3 {
		filter = args[1]
		fn = args[2]
	} else {
		filter = nil
		fn = args[1]
	}

	accuExpr := p.newIdent(ctx, accumulatorName)
	init := p.newList(ctx)
	condition := p.newLiteralBool(ctx, true)
	// TODO: use compiler internal method for faster, stateful add.
	step := p.newGlobalCall(ctx, operators.Add, accuExpr, p.newList(ctx, fn))

	if filter != nil {
		step = p.newGlobalCall(ctx, operators.Conditional, filter, step, accuExpr)
	}
	return p.newComprehension(ctx, v, target, accumulatorName, init, condition, step, accuExpr), nil
}

func makeFilter(p *parserHelper, ctx interface{}, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	v, found := extractIdent(args[0])
	if !found {
		return nil, &common.Error{Message: "argument is not an identifier"}
	}

	filter := args[1]
	accuExpr := p.newIdent(ctx, accumulatorName)
	init := p.newList(ctx)
	condition := p.newLiteralBool(ctx, true)
	// TODO: use compiler internal method for faster, stateful add.
	step := p.newGlobalCall(ctx, operators.Add, accuExpr, p.newList(ctx, args[0]))
	step = p.newGlobalCall(ctx, operators.Conditional, filter, step, accuExpr)
	return p.newComprehension(ctx, v, target, accumulatorName, init, condition, step, accuExpr), nil
}

func extractIdent(e *exprpb.Expr) (string, bool) {
	switch e.ExprKind.(type) {
	case *exprpb.Expr_IdentExpr:
		return e.GetIdentExpr().Name, true
	}
	return "", false
}
