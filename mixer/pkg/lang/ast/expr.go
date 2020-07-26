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

package ast

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"time"

	dpb "istio.io/api/policy/v1beta1"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

// This private variable is an extract from go/token
// it maps expression related token to its names
var tMap = map[token.Token]string{
	token.ILLEGAL: "ILLEGAL",

	token.EOF:     "EOF",
	token.COMMENT: "COMMENT",

	token.IDENT:  "IDENT",
	token.INT:    "INT",
	token.FLOAT:  "FLOAT",
	token.IMAG:   "IMAG",
	token.CHAR:   "CHAR",
	token.STRING: "STRING",

	token.ADD: "ADD",
	token.SUB: "SUB",
	token.MUL: "MUL",
	token.QUO: "QUO",
	token.REM: "REM",

	token.AND:  "AND",
	token.OR:   "OR",
	token.XOR:  "XOR",
	token.LAND: "LAND",
	token.LOR:  "LOR",

	token.EQL: "EQ",
	token.LSS: "LT",
	token.GTR: "GT",
	token.NOT: "NOT",

	token.NEQ: "NEQ",
	token.LEQ: "LEQ",
	token.GEQ: "GEQ",

	token.LBRACK: "INDEX",
}

var typeMap = map[token.Token]dpb.ValueType{
	token.INT:    dpb.INT64,
	token.FLOAT:  dpb.DOUBLE,
	token.CHAR:   dpb.STRING,
	token.STRING: dpb.STRING,
}

// Expression is a simplified expression AST
type Expression struct {
	// Oneof the following
	Const *Constant
	Var   *Variable
	Fn    *Function
}

// EvalType Function an expression using fMap and attribute vocabulary. Returns the type that this expression evaluates to.
func (e *Expression) EvalType(attrs attribute.AttributeDescriptorFinder, fMap map[string]FunctionMetadata) (valueType dpb.ValueType, err error) {
	if e.Const != nil {
		return e.Const.Type, nil
	}
	if e.Var != nil {
		ad := attrs.GetAttribute(e.Var.Name)
		if ad == nil {
			return valueType, fmt.Errorf("unknown attribute %s", e.Var.Name)
		}
		return ad.ValueType, nil
	}
	return e.Fn.EvalType(attrs, fMap)
}

// String produces postfix version with all operators converted to function names
func (e *Expression) String() string {
	if e.Const != nil {
		return e.Const.String()
	}
	if e.Var != nil {
		return e.Var.String()
	}
	if e.Fn != nil {
		return e.Fn.String()
	}
	return "<nil>"
}

// newConstant converts literals recognized by parser (ie. int and string) to
// `dpb.ValueType`s, building a typed *Constant.
func newConstant(v string, vType dpb.ValueType) (*Constant, error) {
	var typedVal interface{}
	var err error
	switch vType {
	case dpb.INT64:
		if typedVal, err = strconv.ParseInt(v, 10, 64); err != nil {
			return nil, err
		}
	case dpb.DOUBLE:
		if typedVal, err = strconv.ParseFloat(v, 64); err != nil {
			return nil, err
		}
	default: // string
		// Several `dpb.ValueType`s are parsed as strings, so
		// they must be parse separately as those value types
		// (and the appropriate vType must be set).
		var unquoted string
		if unquoted, err = strconv.Unquote(v); err != nil {
			return nil, err
		}
		if typedVal, err = time.ParseDuration(unquoted); err == nil {
			vType = dpb.DURATION
			break
		}
		// TODO: add support for other dpb ValueTypes serialized
		// as string
		typedVal = unquoted
	}
	return &Constant{StrValue: v, Type: vType, Value: typedVal}, nil
}

// Constant models a typed constant.
type Constant struct {
	StrValue string
	Value    interface{}
	Type     dpb.ValueType
}

func (c *Constant) String() string {
	return c.StrValue
}

// Variable models a variable.
type Variable struct {
	Name string
}

func (v *Variable) String() string {
	return "$" + v.Name
}

// Function models a function with multiple parameters
// 1st arg can be thought of as the receiver.
type Function struct {
	Name   string
	Target *Expression
	Args   []*Expression
}

func (f *Function) String() string {
	w := pool.GetBuffer()
	if f.Target != nil {
		w.WriteString(f.Target.String())
		w.WriteString(":")
	}
	w.WriteString(f.Name + "(")
	for idx, arg := range f.Args {
		if idx != 0 {
			w.WriteString(", ")
		}
		w.WriteString(arg.String())
	}
	w.WriteString(")")
	s := w.String()
	pool.PutBuffer(w)
	return s
}

// EvalType Function using fMap and attribute vocabulary. Return static or computed return type if all args have correct type.
func (f *Function) EvalType(attrs attribute.AttributeDescriptorFinder, fMap map[string]FunctionMetadata) (valueType dpb.ValueType, err error) {
	fn, found := fMap[f.Name]
	if !found {
		return valueType, fmt.Errorf("unknown function: %s", f.Name)
	}

	tmplType := dpb.VALUE_TYPE_UNSPECIFIED

	if f.Target != nil {
		if !fn.Instance {
			return valueType, fmt.Errorf("invoking regular function on instance method: %s", f.Name)
		}

		var targetType dpb.ValueType
		targetType, err = f.Target.EvalType(attrs, fMap)
		if err != nil {
			return valueType, err
		}

		if fn.TargetType == dpb.VALUE_TYPE_UNSPECIFIED {
			// all future args must be of this type.
			tmplType = targetType
		} else if targetType != fn.TargetType {
			return valueType, fmt.Errorf("%s target typeError got %s, expected %s", f, targetType, fn.TargetType)
		}
	} else if fn.Instance {
		return valueType, fmt.Errorf("invoking instance method without an instance: %s", f.Name)
	}

	var idx int
	argTypes := fn.ArgumentTypes

	if len(f.Args) < len(argTypes) {
		return valueType, fmt.Errorf("%s arity mismatch. Got %d arg(s), expected %d arg(s)", f, len(f.Args), len(argTypes))
	}

	var argType dpb.ValueType
	// check arg types with fn args
	for idx = 0; idx < len(f.Args) && idx < len(argTypes); idx++ {
		argType, err = f.Args[idx].EvalType(attrs, fMap)
		if err != nil {
			return valueType, err
		}
		expectedType := argTypes[idx]
		if expectedType == dpb.VALUE_TYPE_UNSPECIFIED {
			if tmplType == dpb.VALUE_TYPE_UNSPECIFIED {
				// all future args must be of this type.
				tmplType = argType
				continue
			}
			expectedType = tmplType
		}
		if argType != expectedType {
			return valueType, fmt.Errorf("%s arg %d (%s) typeError got %s, expected %s", f, idx+1, f.Args[idx], argType, expectedType)
		}
	}

	// TODO check if we have excess args, only works when Fn is Variadic

	retType := fn.ReturnType
	if retType == dpb.VALUE_TYPE_UNSPECIFIED {
		// if return type is unspecified, use the discovered type
		retType = tmplType
	}

	return retType, nil
}

func generateVarName(selectors []string) string {
	// a.b.c.d is a selector expression
	// normally one walks down a chain of objects
	// we have chosen an internally flat namespace, therefore
	// a.b.c.d if an identifier. converts
	// a.b.c.d --> $a.b.c.d
	// for selectorExpr length is guaranteed to be at least 2.
	ww := pool.GetBuffer()
	ww.WriteString(selectors[len(selectors)-1])
	for idx := len(selectors) - 2; idx >= 0; idx-- {
		ww.WriteString("." + selectors[idx])
	}
	s := ww.String()
	pool.PutBuffer(ww)
	return s
}

func process(ex ast.Expr, tgt *Expression) (err error) {
	switch v := ex.(type) {
	case *ast.UnaryExpr:
		tgt.Fn = &Function{Name: tMap[v.Op]}
		if err = processFunc(tgt.Fn, []ast.Expr{v.X}); err != nil {
			return
		}
	case *ast.BinaryExpr:
		tgt.Fn = &Function{Name: tMap[v.Op]}
		if err = processFunc(tgt.Fn, []ast.Expr{v.X, v.Y}); err != nil {
			return
		}
	case *ast.CallExpr:
		switch tg := v.Fun.(type) {
		case *ast.SelectorExpr:
			var anchorExpr ast.Expr
			var w []string
			anchorExpr, w, err = flattenSelectors(v.Fun.(*ast.SelectorExpr))
			if err != nil {
				return err
			}

			if anchorExpr == nil {
				// This is a simple expression of the form $(ident).$(select)...fn(...)
				instance := &Expression{Var: &Variable{Name: generateVarName(w[1:])}}
				tgt.Fn = &Function{Name: w[0], Target: instance}
			} else {
				afn := &Expression{}
				err = process(anchorExpr, afn)
				if err != nil {
					return err
				}
				if len(w) != 1 {
					return fmt.Errorf("unexpected expression: %#v", v.Fun)
				}
				tgt.Fn = &Function{Name: w[0], Target: afn}
			}
			err = processFunc(tgt.Fn, v.Args)
			return err

		case *ast.Ident:
			tgt.Fn = &Function{Name: tg.Name}
			err = processFunc(tgt.Fn, v.Args)
			return
		}
	case *ast.ParenExpr:
		if err = process(v.X, tgt); err != nil {
			return
		}
	case *ast.BasicLit:
		tgt.Const, err = newConstant(v.Value, typeMap[v.Kind])
		if err != nil {
			return
		}
	case *ast.Ident:
		// true and false are treated as identifiers by parser
		// we need to convert them into constants here
		lv := strings.ToLower(v.Name)
		if lv == "true" || lv == "false" {
			typedVal := true
			if lv == "false" {
				typedVal = false
			}
			tgt.Const = &Constant{StrValue: lv, Type: dpb.BOOL, Value: typedVal}
		} else {
			tgt.Var = &Variable{Name: v.Name}
		}
	case *ast.SelectorExpr:
		var anchorExpr ast.Expr
		var w []string
		anchorExpr, w, err = flattenSelectors(v)
		if err != nil {
			return err
		}
		if anchorExpr != nil {
			return fmt.Errorf("unexpected expression: %#v", v)
		}

		// This is a simple expression of the form $(ident).$(select)...
		tgt.Var = &Variable{Name: generateVarName(w)}

		return nil

	case *ast.IndexExpr:
		// accessing a map
		// request.headers["abc"]
		tgt.Fn = &Function{Name: tMap[token.LBRACK]}
		if err = processFunc(tgt.Fn, []ast.Expr{v.X, v.Index}); err != nil {
			return
		}
	default:
		return fmt.Errorf("unexpected expression: %#v", v)
	}

	return nil
}

func flattenSelectors(selector *ast.SelectorExpr) (ast.Expr, []string, error) {
	var anchor ast.Expr
	parts := []string{}
	ex := selector

	for {
		parts = append(parts, ex.Sel.Name)

		switch v := ex.X.(type) {
		case *ast.SelectorExpr:
			ex = v

		case *ast.Ident:
			parts = append(parts, v.Name)
			return anchor, parts, nil

		case *ast.CallExpr, *ast.BasicLit, *ast.ParenExpr:
			anchor = ex.X
			return anchor, parts, nil

		default:
			return nil, nil, fmt.Errorf("unexpected expression: %#v", v)
		}
	}
}

func processFunc(fn *Function, args []ast.Expr) (err error) {
	fAargs := []*Expression{}
	for _, ee := range args {
		aex := &Expression{}
		fAargs = append(fAargs, aex)
		if err = process(ee, aex); err != nil {
			return
		}
	}
	fn.Args = fAargs
	return nil
}

// Parse parses a given expression to ast.Expression.
func Parse(src string) (ex *Expression, err error) {
	a, err := parser.ParseExpr(src)
	if err != nil {
		return nil, fmt.Errorf("unable to parse expression '%s': %v", src, err)
	}
	log.Debugf("Parsed expression '%s' into '%v'", src, a)

	ex = &Expression{}
	if err = process(a, ex); err != nil {
		return nil, err
	}
	return ex, nil
}

// ExtractEQMatches extracts equality sub expressions from the match expression.
// It only extracts `attribute == literal` type equality matches.
// It returns a list of  <attribute name, value> such that
// if **any** of these comparisons is false, the expression will evaluate to false.
// These sub expressions can be hoisted out of the match clause and evaluated separately.
// For example
// destination.service == "abc"  -- Used to index rules by destination service.
// context.protocol == "tcp"  -- Used to filter rules by context
func ExtractEQMatches(src string) (map[string]interface{}, error) {
	ex, err := Parse(src)
	if err != nil {
		return nil, err
	}
	eqMap := make(map[string]interface{})
	extractEQMatches(ex, eqMap)
	return eqMap, nil
}

func recordIfEQ(fn *Function, eqMap map[string]interface{}) {
	if fn.Name != "EQ" {
		return
	}

	// x == "y"
	if fn.Args[0].Var != nil && fn.Args[1].Const != nil {
		eqMap[fn.Args[0].Var.Name] = fn.Args[1].Const.Value
		return
	}

	// yoda style, "y" == x
	if fn.Args[0].Const != nil && fn.Args[1].Var != nil {
		eqMap[fn.Args[1].Var.Name] = fn.Args[0].Const.Value
	}
}

// parseEQMatches traverse down "LANDS" and record EQs of variable and constants.
func extractEQMatches(ex *Expression, eqMap map[string]interface{}) {
	if ex.Fn == nil {
		return
	}

	recordIfEQ(ex.Fn, eqMap)

	// only recurse on AND function.
	if ex.Fn.Name != "LAND" {
		return
	}

	//TODO remove collected equality expressions from AST
	for _, arg := range ex.Fn.Args {
		extractEQMatches(arg, eqMap)
	}
}
