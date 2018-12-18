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

// Package parser declares an expression parser with support for macro
// expansion.
package parser

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/antlr/antlr4/runtime/Go/antlr"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/parser/gen"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Parse converts a source input a parsed expression.
// This function calls ParseWithMacros with AllMacros.
func Parse(source common.Source) (*exprpb.ParsedExpr, *common.Errors) {
	return ParseWithMacros(source, AllMacros)
}

// ParseWithMacros converts a source input and macros set to a parsed expression.
func ParseWithMacros(source common.Source, macros Macros) (*exprpb.ParsedExpr, *common.Errors) {
	macroMap := make(map[string]Macro)
	for _, m := range macros {
		macroMap[makeMacroKey(m.name, m.args, m.instanceStyle)] = m
	}
	p := parser{
		errors: &parseErrors{common.NewErrors(source)},
		helper: newParserHelper(source),
		macros: macroMap,
	}
	e := p.parse(source.Content())
	return &exprpb.ParsedExpr{
		Expr:       e,
		SourceInfo: p.helper.getSourceInfo(),
	}, p.errors.Errors
}

type parser struct {
	gen.BaseCELVisitor
	errors *parseErrors
	helper *parserHelper
	macros map[string]Macro
}

var _ gen.CELVisitor = (*parser)(nil)

func (p *parser) parse(expression string) *exprpb.Expr {
	stream := antlr.NewInputStream(expression)
	lexer := gen.NewCELLexer(stream)
	prsr := gen.NewCELParser(antlr.NewCommonTokenStream(lexer, 0))

	lexer.RemoveErrorListeners()
	prsr.RemoveErrorListeners()
	lexer.AddErrorListener(p)
	prsr.AddErrorListener(p)

	return p.Visit(prsr.Start()).(*exprpb.Expr)
}

// Visitor implementations.
func (p *parser) Visit(tree antlr.ParseTree) interface{} {
	switch tree.(type) {
	case *gen.StartContext:
		return p.VisitStart(tree.(*gen.StartContext))
	case *gen.ExprContext:
		return p.VisitExpr(tree.(*gen.ExprContext))
	case *gen.ConditionalAndContext:
		return p.VisitConditionalAnd(tree.(*gen.ConditionalAndContext))
	case *gen.ConditionalOrContext:
		return p.VisitConditionalOr(tree.(*gen.ConditionalOrContext))
	case *gen.RelationContext:
		return p.VisitRelation(tree.(*gen.RelationContext))
	case *gen.CalcContext:
		return p.VisitCalc(tree.(*gen.CalcContext))
	case *gen.LogicalNotContext:
		return p.VisitLogicalNot(tree.(*gen.LogicalNotContext))
	case *gen.MemberExprContext:
		return p.VisitMemberExpr(tree.(*gen.MemberExprContext))
	case *gen.PrimaryExprContext:
		return p.VisitPrimaryExpr(tree.(*gen.PrimaryExprContext))
	case *gen.SelectOrCallContext:
		return p.VisitSelectOrCall(tree.(*gen.SelectOrCallContext))
	case *gen.MapInitializerListContext:
		return p.VisitMapInitializerList(tree.(*gen.MapInitializerListContext))
	case *gen.NegateContext:
		return p.VisitNegate(tree.(*gen.NegateContext))
	case *gen.IndexContext:
		return p.VisitIndex(tree.(*gen.IndexContext))
	case *gen.UnaryContext:
		return p.VisitUnary(tree.(*gen.UnaryContext))
	}

	text := "<<nil>>"
	if tree != nil {
		text = tree.GetText()
	}
	panic(fmt.Sprintf("unknown parsetree type: '%+v': %+v [%s]", reflect.TypeOf(tree), tree, text))
}

// Visit a parse tree produced by CELParser#start.
func (p *parser) VisitStart(ctx *gen.StartContext) interface{} {
	return p.Visit(ctx.Expr())
}

// Visit a parse tree produced by CELParser#expr.
func (p *parser) VisitExpr(ctx *gen.ExprContext) interface{} {
	result := p.Visit(ctx.GetE()).(*exprpb.Expr)
	if ctx.GetOp() == nil {
		return result
	}

	ifTrue := p.Visit(ctx.GetE1()).(*exprpb.Expr)
	ifFalse := p.Visit(ctx.GetE2()).(*exprpb.Expr)
	return p.globalCallOrMacro(ctx.GetOp(), operators.Conditional, result, ifTrue, ifFalse)
}

// Visit a parse tree produced by CELParser#conditionalOr.
func (p *parser) VisitConditionalOr(ctx *gen.ConditionalOrContext) interface{} {
	result := p.Visit(ctx.GetE()).(*exprpb.Expr)
	if ctx.GetOps() == nil {
		return result
	}
	for i, op := range ctx.GetOps() {
		next := p.Visit(ctx.GetE1()[i]).(*exprpb.Expr)
		result = p.globalCallOrMacro(op, operators.LogicalOr, result, next)
	}
	return result
}

// Visit a parse tree produced by CELParser#conditionalAnd.
func (p *parser) VisitConditionalAnd(ctx *gen.ConditionalAndContext) interface{} {
	result := p.Visit(ctx.GetE()).(*exprpb.Expr)
	if ctx.GetOps() == nil {
		return result
	}
	for i, op := range ctx.GetOps() {
		next := p.Visit(ctx.GetE1()[i]).(*exprpb.Expr)
		result = p.globalCallOrMacro(op, operators.LogicalAnd, result, next)
	}
	return result
}

// Visit a parse tree produced by CELParser#relation.
func (p *parser) VisitRelation(ctx *gen.RelationContext) interface{} {
	if ctx.Calc() != nil {
		return p.Visit(ctx.Calc())
	}
	opText := ""
	if ctx.GetOp() != nil {
		opText = ctx.GetOp().GetText()
	}

	if op, found := operators.Find(opText); found {
		lhs := p.Visit(ctx.Relation(0)).(*exprpb.Expr)
		rhs := p.Visit(ctx.Relation(1)).(*exprpb.Expr)
		return p.globalCallOrMacro(ctx.GetOp(), op, lhs, rhs)
	}
	return p.reportError(ctx, "operator not found")
}

// Visit a parse tree produced by CELParser#calc.
func (p *parser) VisitCalc(ctx *gen.CalcContext) interface{} {
	if ctx.Unary() != nil {
		return p.Visit(ctx.Unary())
	}
	opText := ""
	if ctx.GetOp() != nil {
		opText = ctx.GetOp().GetText()
	}
	if op, found := operators.Find(opText); found {
		lhs := p.Visit(ctx.Calc(0)).(*exprpb.Expr)
		rhs := p.Visit(ctx.Calc(1)).(*exprpb.Expr)
		return p.globalCallOrMacro(ctx.GetOp(), op, lhs, rhs)
	}
	return p.reportError(ctx, "operator not found")
}

func (p *parser) VisitUnary(ctx *gen.UnaryContext) interface{} {
	return p.helper.newLiteralString(ctx, "<<error>>")
}

// Visit a parse tree produced by CELParser#MemberExpr.
func (p *parser) VisitMemberExpr(ctx *gen.MemberExprContext) interface{} {
	switch ctx.Member().(type) {
	case *gen.PrimaryExprContext:
		return p.VisitPrimaryExpr(ctx.Member().(*gen.PrimaryExprContext))
	case *gen.SelectOrCallContext:
		return p.VisitSelectOrCall(ctx.Member().(*gen.SelectOrCallContext))
	case *gen.IndexContext:
		return p.VisitIndex(ctx.Member().(*gen.IndexContext))
	case *gen.CreateMessageContext:
		return p.VisitCreateMessage(ctx.Member().(*gen.CreateMessageContext))
	}
	return p.reportError(ctx, "unsupported simple expression")
}

// Visit a parse tree produced by CELParser#LogicalNot.
func (p *parser) VisitLogicalNot(ctx *gen.LogicalNotContext) interface{} {
	if len(ctx.GetOps())%2 == 0 {
		return p.Visit(ctx.Member())
	}
	target := p.Visit(ctx.Member()).(*exprpb.Expr)
	return p.globalCallOrMacro(ctx.GetOps()[0], operators.LogicalNot, target)
}

func (p *parser) VisitNegate(ctx *gen.NegateContext) interface{} {
	if len(ctx.GetOps())%2 == 0 {
		return p.Visit(ctx.Member())
	}
	target := p.Visit(ctx.Member()).(*exprpb.Expr)
	return p.globalCallOrMacro(ctx.GetOps()[0], operators.Negate, target)
}

// Visit a parse tree produced by CELParser#SelectOrCall.
func (p *parser) VisitSelectOrCall(ctx *gen.SelectOrCallContext) interface{} {
	operand := p.Visit(ctx.Member()).(*exprpb.Expr)
	// Handle the error case where no valid identifier is specified.
	if ctx.GetId() == nil {
		return p.helper.newExpr(ctx)
	}
	id := ctx.GetId().GetText()
	if ctx.GetOpen() != nil {
		return p.memberCallOrMacro(ctx.GetOpen(), id, operand, p.visitList(ctx.GetArgs())...)
	}
	return p.helper.newSelect(ctx.GetOp(), operand, id)
}

// Visit a parse tree produced by CELParser#PrimaryExpr.
func (p *parser) VisitPrimaryExpr(ctx *gen.PrimaryExprContext) interface{} {
	switch ctx.Primary().(type) {
	case *gen.NestedContext:
		return p.VisitNested(ctx.Primary().(*gen.NestedContext))
	case *gen.IdentOrGlobalCallContext:
		return p.VisitIdentOrGlobalCall(ctx.Primary().(*gen.IdentOrGlobalCallContext))
	case *gen.CreateListContext:
		return p.VisitCreateList(ctx.Primary().(*gen.CreateListContext))
	case *gen.CreateStructContext:
		return p.VisitCreateStruct(ctx.Primary().(*gen.CreateStructContext))
	case *gen.ConstantLiteralContext:
		return p.VisitConstantLiteral(ctx.Primary().(*gen.ConstantLiteralContext))
	}

	return p.reportError(ctx, "invalid primary expression")
}

// Visit a parse tree produced by CELParser#Index.
func (p *parser) VisitIndex(ctx *gen.IndexContext) interface{} {
	target := p.Visit(ctx.Member()).(*exprpb.Expr)
	index := p.Visit(ctx.GetIndex()).(*exprpb.Expr)
	return p.globalCallOrMacro(ctx.GetOp(), operators.Index, target, index)
}

// Visit a parse tree produced by CELParser#CreateMessage.
func (p *parser) VisitCreateMessage(ctx *gen.CreateMessageContext) interface{} {
	target := p.Visit(ctx.Member()).(*exprpb.Expr)
	if messageName, found := p.extractQualifiedName(target); found {
		entries := p.VisitIFieldInitializerList(ctx.GetEntries()).([]*exprpb.Expr_CreateStruct_Entry)
		return p.helper.newObject(ctx, messageName, entries...)
	}
	return p.helper.newExpr(ctx)
}

func (p *parser) VisitIFieldInitializerList(ctx gen.IFieldInitializerListContext) interface{} {
	if ctx == nil || ctx.GetFields() == nil {
		return []*exprpb.Expr_CreateStruct_Entry{}
	}

	result := make([]*exprpb.Expr_CreateStruct_Entry, len(ctx.GetFields()))
	for i, f := range ctx.GetFields() {
		value := p.Visit(ctx.GetValues()[i]).(*exprpb.Expr)
		field := p.helper.newObjectField(ctx.GetCols()[i], f.GetText(), value)
		result[i] = field
	}
	return result
}

// Visit a parse tree produced by CELParser#IdentOrGlobalCall.
func (p *parser) VisitIdentOrGlobalCall(ctx *gen.IdentOrGlobalCallContext) interface{} {
	identName := ""
	if ctx.GetLeadingDot() != nil {
		identName = "."
	}
	// Handle the error case where no valid identifier is specified.
	if ctx.GetId() == nil {
		return p.helper.newExpr(ctx)
	}
	identName += ctx.GetId().GetText()

	if ctx.GetOp() != nil {
		return p.globalCallOrMacro(ctx.GetOp(), identName, p.visitList(ctx.GetArgs())...)
	}
	return p.helper.newIdent(ctx.GetId(), identName)
}

// Visit a parse tree produced by CELParser#Nested.
func (p *parser) VisitNested(ctx *gen.NestedContext) interface{} {
	return p.Visit(ctx.GetE())
}

// Visit a parse tree produced by CELParser#CreateList.
func (p *parser) VisitCreateList(ctx *gen.CreateListContext) interface{} {
	return p.helper.newList(ctx, p.visitList(ctx.GetElems())...)
}

// Visit a parse tree produced by CELParser#CreateStruct.
func (p *parser) VisitCreateStruct(ctx *gen.CreateStructContext) interface{} {
	entries := []*exprpb.Expr_CreateStruct_Entry{}
	if ctx.GetEntries() != nil {
		entries = p.Visit(ctx.GetEntries()).([]*exprpb.Expr_CreateStruct_Entry)
	}
	return p.helper.newMap(ctx.GetStart(), entries...)
}

// Visit a parse tree produced by CELParser#ConstantLiteral.
func (p *parser) VisitConstantLiteral(ctx *gen.ConstantLiteralContext) interface{} {
	switch ctx.Literal().(type) {
	case *gen.IntContext:
		return p.VisitInt(ctx.Literal().(*gen.IntContext))
	case *gen.UintContext:
		return p.VisitUint(ctx.Literal().(*gen.UintContext))
	case *gen.DoubleContext:
		return p.VisitDouble(ctx.Literal().(*gen.DoubleContext))
	case *gen.StringContext:
		return p.VisitString(ctx.Literal().(*gen.StringContext))
	case *gen.BytesContext:
		return p.VisitBytes(ctx.Literal().(*gen.BytesContext))
	case *gen.BoolFalseContext:
		return p.VisitBoolFalse(ctx.Literal().(*gen.BoolFalseContext))
	case *gen.BoolTrueContext:
		return p.VisitBoolTrue(ctx.Literal().(*gen.BoolTrueContext))
	case *gen.NullContext:
		return p.VisitNull(ctx.Literal().(*gen.NullContext))
	}
	return p.reportError(ctx, "invalid literal")
}

// Visit a parse tree produced by CELParser#exprList.
func (p *parser) VisitExprList(ctx *gen.ExprListContext) interface{} {
	if ctx == nil || ctx.GetE() == nil {
		return []*exprpb.Expr{}
	}

	result := make([]*exprpb.Expr, len(ctx.GetE()))
	for i, e := range ctx.GetE() {
		exp := p.Visit(e).(*exprpb.Expr)
		result[i] = exp
	}
	return result
}

// Visit a parse tree produced by CELParser#mapInitializerList.
func (p *parser) VisitMapInitializerList(ctx *gen.MapInitializerListContext) interface{} {
	if ctx == nil || ctx.GetKeys() == nil {
		return []*exprpb.Expr_CreateStruct_Entry{}
	}

	result := make([]*exprpb.Expr_CreateStruct_Entry, len(ctx.GetCols()))
	for i, col := range ctx.GetCols() {
		key := p.Visit(ctx.GetKeys()[i]).(*exprpb.Expr)
		value := p.Visit(ctx.GetValues()[i]).(*exprpb.Expr)
		entry := p.helper.newMapEntry(col, key, value)
		result[i] = entry
	}
	return result
}

// Visit a parse tree produced by CELParser#Int.
func (p *parser) VisitInt(ctx *gen.IntContext) interface{} {
	text := ctx.GetTok().GetText()
	if ctx.GetSign() != nil {
		text = ctx.GetSign().GetText() + text
	}
	i, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return p.reportError(ctx, "invalid int literal")
	}
	return p.helper.newLiteralInt(ctx, i)
}

// Visit a parse tree produced by CELParser#Uint.
func (p *parser) VisitUint(ctx *gen.UintContext) interface{} {
	text := ctx.GetTok().GetText()
	// trim the 'u' designator included in the uint literal.
	text = text[:len(text)-1]
	i, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return p.reportError(ctx, "invalid uint literal")
	}
	return p.helper.newLiteralUint(ctx, i)
}

// Visit a parse tree produced by CELParser#Double.
func (p *parser) VisitDouble(ctx *gen.DoubleContext) interface{} {
	txt := ctx.GetTok().GetText()
	if ctx.GetSign() != nil {
		txt = ctx.GetSign().GetText() + txt
	}
	f, err := strconv.ParseFloat(txt, 64)
	if err != nil {
		return p.reportError(ctx, "invalid double literal")
	}
	return p.helper.newLiteralDouble(ctx, f)

}

// Visit a parse tree produced by CELParser#String.
func (p *parser) VisitString(ctx *gen.StringContext) interface{} {
	s := p.unquote(ctx, ctx.GetText())
	return p.helper.newLiteralString(ctx, s)
}

// Visit a parse tree produced by CELParser#Bytes.
func (p *parser) VisitBytes(ctx *gen.BytesContext) interface{} {
	// TODO(ozben): Not sure if this is the right encoding.
	b := []byte(p.unquote(ctx, ctx.GetTok().GetText()[1:]))
	return p.helper.newLiteralBytes(ctx, b)
}

// Visit a parse tree produced by CELParser#BoolTrue.
func (p *parser) VisitBoolTrue(ctx *gen.BoolTrueContext) interface{} {
	return p.helper.newLiteralBool(ctx, true)
}

// Visit a parse tree produced by CELParser#BoolFalse.
func (p *parser) VisitBoolFalse(ctx *gen.BoolFalseContext) interface{} {
	return p.helper.newLiteralBool(ctx, false)
}

// Visit a parse tree produced by CELParser#Null.
func (p *parser) VisitNull(ctx *gen.NullContext) interface{} {
	return p.helper.newLiteral(ctx,
		&exprpb.Constant{
			ConstantKind: &exprpb.Constant_NullValue{
				NullValue: structpb.NullValue_NULL_VALUE}})
}

func (p *parser) visitList(ctx gen.IExprListContext) []*exprpb.Expr {
	if ctx == nil {
		return []*exprpb.Expr{}
	}
	return p.visitSlice(ctx.GetE())
}

func (p *parser) visitSlice(expressions []gen.IExprContext) []*exprpb.Expr {
	if expressions == nil {
		return []*exprpb.Expr{}
	}
	result := make([]*exprpb.Expr, len(expressions))
	for i, e := range expressions {
		ex := p.Visit(e).(*exprpb.Expr)
		result[i] = ex
	}
	return result
}

func (p *parser) extractQualifiedName(e *exprpb.Expr) (string, bool) {
	if e == nil {
		return "", false
	}
	switch e.ExprKind.(type) {
	case *exprpb.Expr_IdentExpr:
		return e.GetIdentExpr().Name, true
	case *exprpb.Expr_SelectExpr:
		s := e.GetSelectExpr()
		if prefix, found := p.extractQualifiedName(s.Operand); found {
			return prefix + "." + s.Field, true
		}
	}
	// TODO: Add a method to Source to get location from character offset.
	location := p.helper.getLocation(e.Id)
	p.reportError(location, "expected a qualified name")
	return "", false
}

func (p *parser) unquote(ctx interface{}, value string) string {
	text, err := unescape(value)
	if err != nil {
		p.reportError(ctx, err.Error())
		return value
	}
	return text
}

func (p *parser) reportError(ctx interface{}, format string, args ...interface{}) *exprpb.Expr {
	var location common.Location
	switch ctx.(type) {
	case common.Location:
		location = ctx.(common.Location)
	case antlr.Token, antlr.ParserRuleContext:
		err := p.helper.newExpr(ctx)
		location = p.helper.getLocation(err.Id)
	}
	err := p.helper.newExpr(ctx)
	// Provide arguments to the report error.
	p.errors.ReportError(location, format, args...)
	return err
}

// ANTLR Parse listener implementations
func (p *parser) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, e antlr.RecognitionException) {
	// TODO: Snippet
	l := common.NewLocation(line, column)
	p.errors.syntaxError(l, msg)
}

func (p *parser) ReportAmbiguity(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex int, exact bool, ambigAlts *antlr.BitSet, configs antlr.ATNConfigSet) {
	// Intentional
}

func (p *parser) ReportAttemptingFullContext(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex int, conflictingAlts *antlr.BitSet, configs antlr.ATNConfigSet) {
	// Intentional
}

func (p *parser) ReportContextSensitivity(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex, prediction int, configs antlr.ATNConfigSet) {
	// Intentional
}

func (p *parser) globalCallOrMacro(ctx interface{}, function string, args ...*exprpb.Expr) *exprpb.Expr {
	if expr, found := p.expandMacro(ctx, function, nil, args...); found {
		return expr
	}
	return p.helper.newGlobalCall(ctx, function, args...)
}

func (p *parser) memberCallOrMacro(ctx interface{}, function string, target *exprpb.Expr, args ...*exprpb.Expr) *exprpb.Expr {
	if expr, found := p.expandMacro(ctx, function, target, args...); found {
		return expr
	}
	return p.helper.newMemberCall(ctx, function, target, args...)
}

func (p *parser) expandMacro(ctx interface{}, function string, target *exprpb.Expr, args ...*exprpb.Expr) (*exprpb.Expr, bool) {
	if macro, found := p.macros[makeMacroKey(function, len(args), target != nil)]; found {
		expr, err := macro.expander(p.helper, ctx, target, args)
		if err != nil {
			if err.Location != nil {
				return p.reportError(err.Location, err.Message), true
			}
			return p.reportError(ctx, err.Message), true
		}
		return expr, true
	}
	return nil, false
}

func makeMacroKey(name string, args int, instanceStyle bool) string {
	return fmt.Sprintf("%s:%d:%v", name, args, instanceStyle)
}
