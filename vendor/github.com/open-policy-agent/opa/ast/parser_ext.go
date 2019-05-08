// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// This file contains extra functions for parsing Rego.
// Most of the parsing is handled by the auto-generated code in
// parser.go, however, there are additional utilities that are
// helpful for dealing with Rego source inputs (e.g., REPL
// statements, source files, etc.)

package ast

import (
	"fmt"

	"github.com/pkg/errors"
)

// MustParseBody returns a parsed body.
// If an error occurs during parsing, panic.
func MustParseBody(input string) Body {
	parsed, err := ParseBody(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseExpr returns a parsed expression.
// If an error occurs during parsing, panic.
func MustParseExpr(input string) *Expr {
	parsed, err := ParseExpr(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseImports returns a slice of imports.
// If an error occurs during parsing, panic.
func MustParseImports(input string) []*Import {
	parsed, err := ParseImports(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseModule returns a parsed module.
// If an error occurs during parsing, panic.
func MustParseModule(input string) *Module {
	parsed, err := ParseModule("", input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParsePackage returns a Package.
// If an error occurs during parsing, panic.
func MustParsePackage(input string) *Package {
	parsed, err := ParsePackage(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseStatements returns a slice of parsed statements.
// If an error occurs during parsing, panic.
func MustParseStatements(input string) []Statement {
	parsed, _, err := ParseStatements("", input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseStatement returns exactly one statement.
// If an error occurs during parsing, panic.
func MustParseStatement(input string) Statement {
	parsed, err := ParseStatement(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseRef returns a parsed reference.
// If an error occurs during parsing, panic.
func MustParseRef(input string) Ref {
	parsed, err := ParseRef(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseRule returns a parsed rule.
// If an error occurs during parsing, panic.
func MustParseRule(input string) *Rule {
	parsed, err := ParseRule(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// MustParseTerm returns a parsed term.
// If an error occurs during parsing, panic.
func MustParseTerm(input string) *Term {
	parsed, err := ParseTerm(input)
	if err != nil {
		panic(err)
	}
	return parsed
}

// ParseRuleFromBody returns a rule if the body can be interpreted as a rule
// definition. Otherwise, an error is returned.
func ParseRuleFromBody(module *Module, body Body) (*Rule, error) {

	if len(body) != 1 {
		return nil, fmt.Errorf("multiple expressions cannot be used for rule head")
	}

	return ParseRuleFromExpr(module, body[0])
}

// ParseRuleFromExpr returns a rule if the expression can be interpreted as a
// rule definition.
func ParseRuleFromExpr(module *Module, expr *Expr) (*Rule, error) {

	if len(expr.With) > 0 {
		return nil, fmt.Errorf("expressions using with keyword cannot be used for rule head")
	}

	if expr.Negated {
		return nil, fmt.Errorf("negated expressions cannot be used for rule head")
	}

	if term, ok := expr.Terms.(*Term); ok {
		switch v := term.Value.(type) {
		case Ref:
			return ParsePartialSetDocRuleFromTerm(module, term)
		default:
			return nil, fmt.Errorf("%v cannot be used for rule name", TypeName(v))
		}
	}

	if !expr.IsEquality() && expr.IsCall() {
		if _, ok := BuiltinMap[expr.Operator().String()]; ok {
			return nil, fmt.Errorf("rule name conflicts with built-in function")
		}
		return ParseRuleFromCallExpr(module, expr.Terms.([]*Term))
	}

	lhs, rhs := expr.Operand(0), expr.Operand(1)

	rule, err := ParseCompleteDocRuleFromEqExpr(module, lhs, rhs)
	if err == nil {
		return rule, nil
	}

	rule, err = ParseRuleFromCallEqExpr(module, lhs, rhs)
	if err == nil {
		return rule, nil
	}

	return ParsePartialObjectDocRuleFromEqExpr(module, lhs, rhs)
}

// ParseCompleteDocRuleFromEqExpr returns a rule if the expression can be
// interpreted as a complete document definition.
func ParseCompleteDocRuleFromEqExpr(module *Module, lhs, rhs *Term) (*Rule, error) {

	var name Var

	if RootDocumentRefs.Contains(lhs) {
		name = lhs.Value.(Ref)[0].Value.(Var)
	} else if v, ok := lhs.Value.(Var); ok {
		name = v
	} else {
		return nil, fmt.Errorf("%v cannot be used for rule name", TypeName(lhs.Value))
	}

	rule := &Rule{
		Location: rhs.Location,
		Head: &Head{
			Location: rhs.Location,
			Name:     name,
			Value:    rhs,
		},
		Body: NewBody(
			NewExpr(BooleanTerm(true).SetLocation(rhs.Location)).SetLocation(rhs.Location),
		),
		Module: module,
	}

	return rule, nil
}

// ParsePartialObjectDocRuleFromEqExpr returns a rule if the expression can be
// interpreted as a partial object document definition.
func ParsePartialObjectDocRuleFromEqExpr(module *Module, lhs, rhs *Term) (*Rule, error) {

	ref, ok := lhs.Value.(Ref)
	if !ok || len(ref) != 2 {
		return nil, fmt.Errorf("%v cannot be used for rule name", TypeName(lhs.Value))
	}

	name := ref[0].Value.(Var)
	key := ref[1]

	rule := &Rule{
		Location: rhs.Location,
		Head: &Head{
			Location: rhs.Location,
			Name:     name,
			Key:      key,
			Value:    rhs,
		},
		Body: NewBody(
			NewExpr(BooleanTerm(true).SetLocation(rhs.Location)).SetLocation(rhs.Location),
		),
		Module: module,
	}

	return rule, nil
}

// ParsePartialSetDocRuleFromTerm returns a rule if the term can be interpreted
// as a partial set document definition.
func ParsePartialSetDocRuleFromTerm(module *Module, term *Term) (*Rule, error) {

	ref, ok := term.Value.(Ref)
	if !ok {
		return nil, fmt.Errorf("%vs cannot be used for rule head", TypeName(term.Value))
	}

	if len(ref) != 2 {
		return nil, fmt.Errorf("refs cannot be used for rule")
	}

	rule := &Rule{
		Location: term.Location,
		Head: &Head{
			Location: term.Location,
			Name:     ref[0].Value.(Var),
			Key:      ref[1],
		},
		Body: NewBody(
			NewExpr(BooleanTerm(true).SetLocation(term.Location)).SetLocation(term.Location),
		),
		Module: module,
	}

	return rule, nil
}

// ParseRuleFromCallEqExpr returns a rule if the term can be interpreted as a
// function definition (e.g., f(x) = y => f(x) = y { true }).
func ParseRuleFromCallEqExpr(module *Module, lhs, rhs *Term) (*Rule, error) {

	call, ok := lhs.Value.(Call)
	if !ok {
		return nil, fmt.Errorf("must be call")
	}

	rule := &Rule{
		Location: lhs.Location,
		Head: &Head{
			Location: lhs.Location,
			Name:     call[0].Value.(Ref)[0].Value.(Var),
			Args:     Args(call[1:]),
			Value:    rhs,
		},
		Body:   NewBody(NewExpr(BooleanTerm(true).SetLocation(rhs.Location)).SetLocation(rhs.Location)),
		Module: module,
	}

	return rule, nil
}

// ParseRuleFromCallExpr returns a rule if the terms can be interpreted as a
// function returning true or some value (e.g., f(x) => f(x) = true { true }).
func ParseRuleFromCallExpr(module *Module, terms []*Term) (*Rule, error) {

	if len(terms) <= 1 {
		return nil, fmt.Errorf("rule argument list must take at least one argument")
	}

	loc := terms[0].Location
	args := terms[1:]
	value := BooleanTerm(true).SetLocation(loc)

	rule := &Rule{
		Location: loc,
		Head: &Head{
			Location: loc,
			Name:     Var(terms[0].String()),
			Args:     args,
			Value:    value,
		},
		Module: module,
		Body:   NewBody(NewExpr(BooleanTerm(true).SetLocation(loc)).SetLocation(loc)),
	}
	return rule, nil
}

// ParseImports returns a slice of Import objects.
func ParseImports(input string) ([]*Import, error) {
	stmts, _, err := ParseStatements("", input)
	if err != nil {
		return nil, err
	}
	result := []*Import{}
	for _, stmt := range stmts {
		if imp, ok := stmt.(*Import); ok {
			result = append(result, imp)
		} else {
			return nil, fmt.Errorf("expected import but got %T", stmt)
		}
	}
	return result, nil
}

// ParseModule returns a parsed Module object.
// For details on Module objects and their fields, see policy.go.
// Empty input will return nil, nil.
func ParseModule(filename, input string) (*Module, error) {
	stmts, comments, err := ParseStatements(filename, input)
	if err != nil {
		return nil, err
	}
	return parseModule(stmts, comments)
}

// ParseBody returns exactly one body.
// If multiple bodies are parsed, an error is returned.
func ParseBody(input string) (Body, error) {
	stmts, _, err := ParseStatements("", input)
	if err != nil {
		return nil, err
	}

	result := Body{}

	for _, stmt := range stmts {
		switch stmt := stmt.(type) {
		case Body:
			result = append(result, stmt...)
		case *Comment:
			// skip
		default:
			return nil, fmt.Errorf("expected body but got %T", stmt)
		}
	}

	setExprIndices(result)

	return result, nil
}

// ParseExpr returns exactly one expression.
// If multiple expressions are parsed, an error is returned.
func ParseExpr(input string) (*Expr, error) {
	body, err := ParseBody(input)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse expression")
	}
	if len(body) != 1 {
		return nil, fmt.Errorf("expected exactly one expression but got: %v", body)
	}
	return body[0], nil
}

// ParsePackage returns exactly one Package.
// If multiple statements are parsed, an error is returned.
func ParsePackage(input string) (*Package, error) {
	stmt, err := ParseStatement(input)
	if err != nil {
		return nil, err
	}
	pkg, ok := stmt.(*Package)
	if !ok {
		return nil, fmt.Errorf("expected package but got %T", stmt)
	}
	return pkg, nil
}

// ParseTerm returns exactly one term.
// If multiple terms are parsed, an error is returned.
func ParseTerm(input string) (*Term, error) {
	body, err := ParseBody(input)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse term")
	}
	if len(body) != 1 {
		return nil, fmt.Errorf("expected exactly one term but got: %v", body)
	}
	term, ok := body[0].Terms.(*Term)
	if !ok {
		return nil, fmt.Errorf("expected term but got %v", body[0].Terms)
	}
	return term, nil
}

// ParseRef returns exactly one reference.
func ParseRef(input string) (Ref, error) {
	term, err := ParseTerm(input)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse ref")
	}
	ref, ok := term.Value.(Ref)
	if !ok {
		return nil, fmt.Errorf("expected ref but got %v", term)
	}
	return ref, nil
}

// ParseRule returns exactly one rule.
// If multiple rules are parsed, an error is returned.
func ParseRule(input string) (*Rule, error) {
	stmts, _, err := ParseStatements("", input)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, fmt.Errorf("expected exactly one statement (rule)")
	}
	rule, ok := stmts[0].(*Rule)
	if !ok {
		return nil, fmt.Errorf("expected rule but got %T", stmts[0])
	}
	return rule, nil
}

// ParseStatement returns exactly one statement.
// A statement might be a term, expression, rule, etc. Regardless,
// this function expects *exactly* one statement. If multiple
// statements are parsed, an error is returned.
func ParseStatement(input string) (Statement, error) {
	stmts, _, err := ParseStatements("", input)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, fmt.Errorf("expected exactly one statement")
	}
	return stmts[0], nil
}

// CommentsOption returns a parser option to initialize the comments store within
// the parser.
func CommentsOption() Option {
	return GlobalStore(commentsKey, []*Comment{})
}

// ParseStatements returns a slice of parsed statements.
// This is the default return value from the parser.
func ParseStatements(filename, input string) ([]Statement, []*Comment, error) {

	parsed, err := Parse(filename, []byte(input), GlobalStore(filenameKey, filename), CommentsOption())
	if err != nil {
		switch err := err.(type) {
		case errList:
			return nil, nil, convertErrList(filename, err)
		default:
			return nil, nil, err
		}
	}

	var comments []*Comment
	var sl []interface{}
	if p, ok := parsed.(program); ok {
		sl = p.buf
		comments = p.comments.([]*Comment)
	} else {
		sl = parsed.([]interface{})
	}
	stmts := make([]Statement, 0, len(sl))

	for _, x := range sl {
		if rules, ok := x.([]*Rule); ok {
			for _, rule := range rules {
				stmts = append(stmts, rule)
			}
		} else {
			// Unchecked cast should be safe. A panic indicates grammar is
			// out-of-sync.
			stmts = append(stmts, x.(Statement))
		}
	}

	return stmts, comments, postProcess(filename, stmts)
}

func convertErrList(filename string, errs errList) error {
	r := make(Errors, len(errs))
	for i, e := range errs {
		switch e := e.(type) {
		case *parserError:
			r[i] = formatParserError(filename, e)
		default:
			r[i] = NewError(ParseErr, nil, e.Error())
		}
	}
	return r
}

func formatParserError(filename string, e *parserError) *Error {
	loc := NewLocation(nil, filename, e.pos.line, e.pos.col)
	return NewError(ParseErr, loc, e.Inner.Error())
}

func parseModule(stmts []Statement, comments []*Comment) (*Module, error) {

	if len(stmts) == 0 {
		return nil, nil
	}

	var errs Errors

	_package, ok := stmts[0].(*Package)
	if !ok {
		loc := stmts[0].(Statement).Loc()
		errs = append(errs, NewError(ParseErr, loc, "package expected"))
	}

	mod := &Module{
		Package: _package,
	}

	// The comments slice only holds comments that were not their own statements.
	mod.Comments = append(mod.Comments, comments...)

	for _, stmt := range stmts[1:] {
		switch stmt := stmt.(type) {
		case *Import:
			mod.Imports = append(mod.Imports, stmt)
		case *Rule:
			setRuleModule(stmt, mod)
			mod.Rules = append(mod.Rules, stmt)
		case Body:
			rule, err := ParseRuleFromBody(mod, stmt)
			if err != nil {
				errs = append(errs, NewError(ParseErr, stmt[0].Location, err.Error()))
			} else {
				mod.Rules = append(mod.Rules, rule)
			}
		case *Package:
			errs = append(errs, NewError(ParseErr, stmt.Loc(), "unexpected package"))
		case *Comment: // Ignore comments, they're handled above.
		default:
			panic("illegal value") // Indicates grammar is out-of-sync with code.
		}
	}

	if len(errs) == 0 {
		return mod, nil
	}

	return nil, errs
}

func postProcess(filename string, stmts []Statement) error {

	if err := mangleDataVars(stmts); err != nil {
		return err
	}

	if err := mangleInputVars(stmts); err != nil {
		return err
	}

	mangleWildcards(stmts)
	mangleExprIndices(stmts)

	return nil
}

func mangleDataVars(stmts []Statement) error {
	for i := range stmts {
		vt := newVarToRefTransformer(DefaultRootDocument.Value.(Var), DefaultRootRef.Copy())
		stmt, err := Transform(vt, stmts[i])
		if err != nil {
			return err
		}
		stmts[i] = stmt.(Statement)
	}
	return nil
}

func mangleInputVars(stmts []Statement) error {
	for i := range stmts {
		vt := newVarToRefTransformer(InputRootDocument.Value.(Var), InputRootRef.Copy())
		stmt, err := Transform(vt, stmts[i])
		if err != nil {
			return err
		}
		stmts[i] = stmt.(Statement)
	}
	return nil
}

func mangleExprIndices(stmts []Statement) {
	for _, stmt := range stmts {
		setExprIndices(stmt)
	}
}

func setExprIndices(x interface{}) {
	WalkBodies(x, func(b Body) bool {
		for i, expr := range b {
			expr.Index = i
		}
		return false
	})
}

func mangleWildcards(stmts []Statement) {
	m := &wildcardMangler{}
	for i := range stmts {
		stmt, _ := Transform(m, stmts[i])
		stmts[i] = stmt.(Statement)
	}
}

type wildcardMangler struct {
	c int
}

func (m *wildcardMangler) Transform(x interface{}) (interface{}, error) {
	if term, ok := x.(Var); ok {
		if term.Equal(Wildcard.Value) {
			name := fmt.Sprintf("%s%d", WildcardPrefix, m.c)
			m.c++
			return Var(name), nil
		}
	}
	return x, nil
}

func setRuleModule(rule *Rule, module *Module) {
	rule.Module = module
	if rule.Else != nil {
		setRuleModule(rule.Else, module)
	}
}

type varToRefTransformer struct {
	orig   Var
	target Ref
	// skip set to true to avoid recursively processing the result of
	// transformation.
	skip bool
}

func newVarToRefTransformer(orig Var, target Ref) *varToRefTransformer {
	return &varToRefTransformer{
		orig:   orig,
		target: target,
		skip:   false,
	}
}

func (vt *varToRefTransformer) Transform(x interface{}) (interface{}, error) {
	if vt.skip {
		vt.skip = false
		return x, nil
	}
	switch x := x.(type) {
	case *Head:
		// The next AST node will be the rule name (which should not be
		// transformed).
		vt.skip = true
	case Ref:
		// The next AST node will be the ref head (which should not be
		// transformed).
		vt.skip = true
	case Var:
		if x.Equal(vt.orig) {
			vt.skip = true
			return vt.target, nil
		}
	}
	return x, nil
}
