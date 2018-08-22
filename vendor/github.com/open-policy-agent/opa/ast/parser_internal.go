// Copyright 2018 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.op

package ast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
)

const (
	// commentsKey is the global map key for the comments slice.
	commentsKey = "comments"

	// filenameKey is the global map key for the filename.
	filenameKey = "filename"
)

type program struct {
	buf      []interface{}
	comments interface{}
}

type ruleExt struct {
	loc  *Location
	term *Term
	body Body
}

// currentLocation converts the parser context to a Location object.
func currentLocation(c *current) *Location {
	return NewLocation(c.text, c.globalStore[filenameKey].(string), c.pos.line, c.pos.col)
}

func makeProgram(c *current, vals interface{}) (interface{}, error) {
	var buf []interface{}
	if vals == nil {
		return buf, nil
	}
	ifaceSlice := vals.([]interface{})
	head := ifaceSlice[0]
	buf = append(buf, head)
	for _, tail := range ifaceSlice[1].([]interface{}) {
		stmt := tail.([]interface{})[1]
		buf = append(buf, stmt)
	}
	return program{buf, c.globalStore[commentsKey]}, nil
}

func makePackage(loc *Location, value interface{}) (interface{}, error) {
	// All packages are implicitly declared under the default root document.
	term := value.(*Term)
	path := Ref{DefaultRootDocument.Copy().SetLocation(term.Location)}
	switch v := term.Value.(type) {
	case Ref:
		// Convert head of package Ref to String because it will be prefixed
		// with the root document variable.
		head := StringTerm(string(v[0].Value.(Var))).SetLocation(v[0].Location)
		tail := v[1:]
		if !tail.IsGround() {
			return nil, fmt.Errorf("package name cannot contain variables: %v", v)
		}

		// We do not allow non-string values in package names.
		// Because documents are typically represented as JSON, non-string keys are
		// not allowed for now.
		// TODO(tsandall): consider special syntax for namespacing under arrays.
		for _, p := range tail {
			_, ok := p.Value.(String)
			if !ok {
				return nil, fmt.Errorf("package name cannot contain non-string values: %v", v)
			}
		}
		path = append(path, head)
		path = append(path, tail...)
	case Var:
		s := StringTerm(string(v)).SetLocation(term.Location)
		path = append(path, s)
	}
	pkg := &Package{Location: loc, Path: path}
	return pkg, nil
}

func makeImport(loc *Location, path, alias interface{}) (interface{}, error) {
	imp := &Import{}
	imp.Location = loc
	imp.Path = path.(*Term)
	if err := IsValidImportPath(imp.Path.Value); err != nil {
		return nil, err
	}
	if alias == nil {
		return imp, nil
	}
	aliasSlice := alias.([]interface{})
	// Import definition above describes the "alias" slice. We only care about the "Var" element.
	imp.Alias = aliasSlice[3].(*Term).Value.(Var)
	return imp, nil
}

func makeDefaultRule(loc *Location, name, value interface{}) (interface{}, error) {

	term := value.(*Term)
	var err error

	vis := NewGenericVisitor(func(x interface{}) bool {
		if err != nil {
			return true
		}
		switch x.(type) {
		case *ArrayComprehension, *ObjectComprehension, *SetComprehension: // skip closures
			return true
		case Ref, Var:
			err = fmt.Errorf("default rule value cannot contain %v", TypeName(x))
			return true
		}
		return false
	})

	Walk(vis, term)

	if err != nil {
		return nil, err
	}

	body := NewBody(NewExpr(BooleanTerm(true).SetLocation(loc)))

	rule := &Rule{
		Location: loc,
		Default:  true,
		Head: &Head{
			Location: loc,
			Name:     name.(*Term).Value.(Var),
			Value:    value.(*Term),
		},
		Body: body,
	}
	rule.Body[0].Location = loc

	return []*Rule{rule}, nil
}

func makeRule(loc *Location, head, rest interface{}) (interface{}, error) {

	if head == nil {
		return nil, nil
	}

	sl := rest.([]interface{})

	rules := []*Rule{
		&Rule{
			Location: loc,
			Head:     head.(*Head),
			Body:     sl[0].(Body),
		},
	}

	var ordered bool
	prev := rules[0]

	for i, elem := range sl[1].([]interface{}) {

		next := elem.([]interface{})
		re := next[1].(ruleExt)

		if re.term == nil {
			if ordered {
				return nil, fmt.Errorf("expected 'else' keyword")
			}
			rules = append(rules, &Rule{
				Location: re.loc,
				Head:     prev.Head.Copy(),
				Body:     re.body,
			})
		} else {
			if (rules[0].Head.DocKind() != CompleteDoc) || (i != 0 && !ordered) {
				return nil, fmt.Errorf("unexpected 'else' keyword")
			}
			ordered = true
			curr := &Rule{
				Location: re.loc,
				Head: &Head{
					Name:     prev.Head.Name,
					Args:     prev.Head.Args.Copy(),
					Value:    re.term,
					Location: re.term.Location,
				},
				Body: re.body,
			}
			prev.Else = curr
			prev = curr
		}
	}

	return rules, nil
}

func makeRuleHead(loc *Location, name, args, key, value interface{}) (interface{}, error) {

	head := &Head{}

	head.Location = loc
	head.Name = name.(*Term).Value.(Var)

	if args != nil && key != nil {
		return nil, fmt.Errorf("partial rules cannot take arguments")
	}

	if args != nil {
		argSlice := args.([]interface{})
		head.Args = argSlice[3].(Args)
	}

	if key != nil {
		keySlice := key.([]interface{})
		// Head definition above describes the "key" slice. We care about the "Term" element.
		head.Key = keySlice[3].(*Term)
	}

	if value != nil {
		valueSlice := value.([]interface{})
		// Head definition above describes the "value" slice. We care about the "Term" element.
		head.Value = valueSlice[len(valueSlice)-1].(*Term)
	}

	if key == nil && value == nil {
		head.Value = BooleanTerm(true).SetLocation(head.Location)
	}

	if key != nil && value != nil {
		switch head.Key.Value.(type) {
		case Var, String, Ref: // nop
		default:
			return nil, fmt.Errorf("object key must be string, var, or ref, not %v", TypeName(head.Key.Value))
		}
	}

	return head, nil
}

func makeArgs(list interface{}) (interface{}, error) {
	termSlice := list.([]*Term)
	args := make(Args, len(termSlice))
	for i := 0; i < len(args); i++ {
		args[i] = termSlice[i]
	}
	return args, nil
}

func makeRuleExt(loc *Location, val, b interface{}) (interface{}, error) {
	bs := b.([]interface{})
	body := bs[1].(Body)

	if val == nil {
		term := BooleanTerm(true)
		term.Location = loc
		return ruleExt{term.Location, term, body}, nil
	}

	vs := val.([]interface{})
	t := vs[3].(*Term)
	return ruleExt{loc, t, body}, nil
}

func makeLiteral(negated, value, with interface{}) (interface{}, error) {

	expr := value.(*Expr)

	expr.Negated = negated.(bool)

	if with != nil {
		expr.With = with.([]*With)
	}

	return expr, nil
}

func makeLiteralExpr(loc *Location, lhs, rest interface{}) (interface{}, error) {

	if rest == nil {
		if call, ok := lhs.(*Term).Value.(Call); ok {
			return NewExpr([]*Term(call)).SetLocation(loc), nil
		}
		return NewExpr(lhs).SetLocation(loc), nil
	}

	termSlice := rest.([]interface{})
	terms := []*Term{
		termSlice[1].(*Term),
		lhs.(*Term),
		termSlice[3].(*Term),
	}

	expr := NewExpr(terms).SetLocation(loc)

	return expr, nil
}

func makeWithKeywordList(head, tail interface{}) (interface{}, error) {
	var withs []*With

	if head == nil {
		return withs, nil
	}

	sl := tail.([]interface{})

	withs = make([]*With, 0, len(sl)+1)
	withs = append(withs, head.(*With))

	for i := range sl {
		withSlice := sl[i].([]interface{})
		withs = append(withs, withSlice[1].(*With))
	}

	return withs, nil
}

func makeWithKeyword(loc *Location, target, value interface{}) (interface{}, error) {
	w := &With{
		Target: target.(*Term),
		Value:  value.(*Term),
	}
	return w.SetLocation(loc), nil
}

func makeExprTerm(loc *Location, lhs, rest interface{}) (interface{}, error) {

	if rest == nil {
		return lhs, nil
	}

	sl := rest.([]interface{})

	if len(sl) == 0 {
		return lhs, nil
	}

	for i := range sl {
		termSlice := sl[i].([]interface{})
		call := Call{
			termSlice[1].(*Term),
			lhs.(*Term),
			termSlice[3].(*Term),
		}
		lhs = NewTerm(call).SetLocation(loc)
	}

	return lhs, nil
}

func makeCall(loc *Location, operator, args interface{}) (interface{}, error) {

	termSlice := args.([]*Term)
	termOperator := operator.(*Term)

	call := make(Call, len(termSlice)+1)

	if _, ok := termOperator.Value.(Var); ok {
		termOperator = RefTerm(termOperator).SetLocation(loc)
	}

	call[0] = termOperator

	for i := 1; i < len(call); i++ {
		call[i] = termSlice[i-1]
	}

	return NewTerm(call).SetLocation(loc), nil
}

func makeBraceEnclosedBody(loc *Location, body interface{}) (interface{}, error) {
	if body != nil {
		return body, nil
	}
	return NewBody(NewExpr(ObjectTerm().SetLocation(loc)).SetLocation(loc)), nil
}

func makeBody(head, tail interface{}, pos int) (interface{}, error) {

	sl := tail.([]interface{})
	body := make(Body, len(sl)+1)
	body[0] = head.(*Expr)

	for i := 1; i < len(body); i++ {
		body[i] = sl[i-1].([]interface{})[pos].(*Expr)
	}

	return body, nil
}

func makeExprTermList(head, tail interface{}) (interface{}, error) {

	var terms []*Term

	if head == nil {
		return terms, nil
	}

	sl := tail.([]interface{})

	terms = make([]*Term, 0, len(sl)+1)
	terms = append(terms, head.(*Term))

	for i := range sl {
		termSlice := sl[i].([]interface{})
		terms = append(terms, termSlice[3].(*Term))
	}

	return terms, nil
}

func makeExprTermPairList(head, tail interface{}) (interface{}, error) {

	var terms [][2]*Term

	if head == nil {
		return terms, nil
	}

	sl := tail.([]interface{})

	terms = make([][2]*Term, 0, len(sl)+1)
	terms = append(terms, head.([2]*Term))

	for i := range sl {
		termSlice := sl[i].([]interface{})
		terms = append(terms, termSlice[3].([2]*Term))
	}

	return terms, nil
}

func makeExprTermPair(key, value interface{}) (interface{}, error) {
	return [2]*Term{key.(*Term), value.(*Term)}, nil
}

func makeInfixOperator(loc *Location, text []byte) (interface{}, error) {
	op := string(text)
	for _, b := range Builtins {
		if string(b.Infix) == op {
			op = string(b.Name)
		}
	}
	operator := RefTerm(VarTerm(op).SetLocation(loc)).SetLocation(loc)
	return operator, nil
}

func makeArray(loc *Location, list interface{}) (interface{}, error) {
	termSlice := list.([]*Term)
	return ArrayTerm(termSlice...).SetLocation(loc), nil
}

func makeObject(loc *Location, list interface{}) (interface{}, error) {
	termPairSlice := list.([][2]*Term)
	return ObjectTerm(termPairSlice...).SetLocation(loc), nil
}

func makeSet(loc *Location, list interface{}) (interface{}, error) {
	termSlice := list.([]*Term)
	return SetTerm(termSlice...).SetLocation(loc), nil
}

func makeArrayComprehension(loc *Location, head, body interface{}) (interface{}, error) {
	return ArrayComprehensionTerm(head.(*Term), body.(Body)).SetLocation(loc), nil
}

func makeSetComprehension(loc *Location, head, body interface{}) (interface{}, error) {
	return SetComprehensionTerm(head.(*Term), body.(Body)).SetLocation(loc), nil
}

func makeObjectComprehension(loc *Location, head, body interface{}) (interface{}, error) {
	pair := head.([2]*Term)
	return ObjectComprehensionTerm(pair[0], pair[1], body.(Body)).SetLocation(loc), nil
}

func makeRef(loc *Location, head, rest interface{}) (interface{}, error) {

	headTerm := head.(*Term)
	ifaceSlice := rest.([]interface{})

	ref := make(Ref, len(ifaceSlice)+1)
	ref[0] = headTerm

	for i := 1; i < len(ref); i++ {
		ref[i] = ifaceSlice[i-1].(*Term)
	}

	return NewTerm(ref).SetLocation(loc), nil
}

func makeRefOperandDot(loc *Location, val interface{}) (interface{}, error) {
	return StringTerm(string(val.(*Term).Value.(Var))).SetLocation(loc), nil
}

func makeVar(loc *Location, text interface{}) (interface{}, error) {
	str := string(text.([]byte))
	return VarTerm(str).SetLocation(loc), nil
}

func makeNumber(loc *Location, text interface{}) (interface{}, error) {
	f, ok := new(big.Float).SetString(string(text.([]byte)))
	if !ok {
		// This indicates the grammar is out-of-sync with what the string
		// representation of floating point numbers. This should not be
		// possible.
		panic("illegal value")
	}
	return NumberTerm(json.Number(f.String())).SetLocation(loc), nil
}

func makeString(loc *Location, text interface{}) (interface{}, error) {
	var v string
	err := json.Unmarshal(text.([]byte), &v)
	return StringTerm(v).SetLocation(loc), err
}

func makeRawString(loc *Location, text interface{}) (interface{}, error) {
	s := string(text.([]byte))
	s = s[1 : len(s)-1] // Trim surrounding quotes.
	return StringTerm(s).SetLocation(loc), nil
}

func makeBool(loc *Location, text interface{}) (interface{}, error) {
	var term *Term
	if string(text.([]byte)) == "true" {
		term = BooleanTerm(true)
	} else {
		term = BooleanTerm(false)
	}
	return term.SetLocation(loc), nil
}

func makeNull(loc *Location) (interface{}, error) {
	return NullTerm().SetLocation(loc), nil
}

func makeComments(c *current, text interface{}) (interface{}, error) {

	var buf bytes.Buffer
	for _, x := range text.([]interface{}) {
		buf.Write(x.([]byte))
	}

	comment := NewComment(buf.Bytes())
	comment.Location = currentLocation(c)
	comments := c.globalStore[commentsKey].([]*Comment)
	comments = append(comments, comment)
	c.globalStore[commentsKey] = comments

	return comment, nil
}

func ifacesToBody(i interface{}, a ...interface{}) Body {
	var buf Body
	buf = append(buf, i.(*Expr))
	for _, s := range a {
		expr := s.([]interface{})[3].(*Expr)
		buf = append(buf, expr)
	}
	return buf
}
