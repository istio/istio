// Copyright 2017 Google Inc.
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

package expr

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"

	"github.com/golang/glog"

	config "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/attribute"
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

var typeMap = map[token.Token]config.ValueType{
	token.INT:    config.INT64,
	token.FLOAT:  config.DOUBLE,
	token.CHAR:   config.STRING,
	token.STRING: config.STRING,
}

// Expression is a simplified expression AST
type Expression struct {
	// Oneof the following
	Const *Constant
	Var   *Variable
	Fn    *Function
}

// AttributeDescriptorFinder finds attribute descriptors.
type AttributeDescriptorFinder interface {
	// FindAttributeDescriptor finds attribute descriptor in the vocabulary. returns nil if not found.
	FindAttributeDescriptor(name string) *config.AttributeDescriptor
}

// TypeCheck an expression using fMap and attribute vocabulary. Returns the type that this expression evaluates to.
func (e *Expression) TypeCheck(attrs AttributeDescriptorFinder, fMap map[string]Func) (valueType config.ValueType, err error) {
	if e.Const != nil {
		return e.Const.Type, nil
	}
	if e.Var != nil {
		ad := attrs.FindAttributeDescriptor(e.Var.Name)
		if ad == nil {
			return valueType, fmt.Errorf("unresolved attribute %s", e.Var.Name)
		}
		return ad.ValueType, nil
	}
	return e.Fn.TypeCheck(attrs, fMap)
}

// Eval evaluates the expression given an attribute bag and a function map.
func (e *Expression) Eval(attrs attribute.Bag, fMap map[string]Func) (interface{}, error) {
	if e.Const != nil {
		return e.Const.Value, nil
	}
	if e.Var != nil {
		v, ok := attribute.Value(attrs, e.Var.Name)
		if !ok {
			return nil, fmt.Errorf("unresolved attribute %s", e.Var.Name)
		}
		return v, nil
	}
	// may panic
	return e.Fn.Eval(attrs, fMap)
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

// newConstant creates a new constant of given type.
// It also stores a typed form of the constant.
func newConstant(v string, vType config.ValueType) (*Constant, error) {
	var typedVal interface{}
	var err error
	switch vType {
	case config.INT64:
		if typedVal, err = strconv.ParseInt(v, 10, 64); err != nil {
			return nil, err
		}
	case config.DOUBLE:
		if typedVal, err = strconv.ParseFloat(v, 64); err != nil {
			return nil, err
		}
	default: // string
		if typedVal, err = strconv.Unquote(v); err != nil {
			return nil, err
		}
	}
	return &Constant{StrValue: v, Type: vType, Value: typedVal}, nil
}

// Constant models a typed constant.
type Constant struct {
	StrValue string
	Value    interface{}
	Type     config.ValueType
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
	Name string
	Args []*Expression
}

func (f *Function) String() string {
	var w bytes.Buffer
	w.WriteString(f.Name + "(")
	for idx, arg := range f.Args {
		if idx != 0 {
			w.WriteString(", ")
		}
		w.WriteString(arg.String())
	}
	w.WriteString(")")
	return w.String()
}

// Eval evaluate function.
func (f *Function) Eval(attrs attribute.Bag, fMap map[string]Func) (interface{}, error) {
	fn := fMap[f.Name]
	if fn == nil {
		return nil, fmt.Errorf("unknown function: %s", f.Name)
	}
	// may panic if config is not consistent with Func.ArgTypes().
	args := []interface{}{}
	for _, earg := range f.Args {
		arg, err := earg.Eval(attrs, fMap)
		if err != nil && !fn.AcceptsNulls() {
			return nil, err
		}
		args = append(args, arg)
	}
	glog.V(2).Infof("calling %#v %#v", fn, args)
	return fn.Call(args), nil
}

// TypeCheck Function using fMap and attribute vocabulary. Return static or computed return type if all args have correct type.
func (f *Function) TypeCheck(attrs AttributeDescriptorFinder, fMap map[string]Func) (valueType config.ValueType, err error) {
	fn := fMap[f.Name]
	if fn == nil {
		return valueType, fmt.Errorf("unknown function: %s", f.Name)
	}

	var idx int
	argTypes := fn.ArgTypes()

	if len(f.Args) < len(argTypes) {
		return valueType, fmt.Errorf("%s arity mismatch. Got %d arg(s), expected %d arg(s)", f, len(f.Args), len(argTypes))
	}

	var argType config.ValueType
	tmplType := config.VALUE_TYPE_UNSPECIFIED
	// check arg types with fn args
	for idx = 0; idx < len(f.Args) && idx < len(argTypes); idx++ {
		argType, err = f.Args[idx].TypeCheck(attrs, fMap)
		if err != nil {
			return valueType, err
		}
		expectedType := argTypes[idx]
		if expectedType == config.VALUE_TYPE_UNSPECIFIED {
			if tmplType == config.VALUE_TYPE_UNSPECIFIED {
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

	retType := fn.ReturnType()
	if retType == config.VALUE_TYPE_UNSPECIFIED {
		// if return type is unspecified, you the discovered type
		retType = tmplType
	}

	return retType, nil
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
		vfunc, found := v.Fun.(*ast.Ident)
		if !found {
			return fmt.Errorf("unexpected expression: %#v", v.Fun)
		}
		tgt.Fn = &Function{Name: vfunc.Name}
		if err = processFunc(tgt.Fn, v.Args); err != nil {
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
			tgt.Const = &Constant{StrValue: lv, Type: config.BOOL, Value: typedVal}
		} else {
			tgt.Var = &Variable{Name: v.Name}
		}
	case *ast.SelectorExpr:
		// a.b.c.d is a selector expression
		// normally one walks down a chain of objects
		// we have chosen an internally flat namespace, therefore
		// a.b.c.d if an identifer. converts
		// a.b.c.d --> $a.b.c.d
		// for selectorExpr length is guaranteed to be at least 2.
		var w []string
		if err = processSelectorExpr(v, &w); err != nil {
			return
		}
		var ww bytes.Buffer
		ww.WriteString(w[len(w)-1])
		for idx := len(w) - 2; idx >= 0; idx-- {
			ww.WriteString("." + w[idx])
		}
		tgt.Var = &Variable{Name: ww.String()}
	case *ast.IndexExpr:
		// accessing a map
		// request.header["abc"]
		tgt.Fn = &Function{Name: tMap[token.LBRACK]}
		if err = processFunc(tgt.Fn, []ast.Expr{v.X, v.Index}); err != nil {
			return
		}
	default:
		return fmt.Errorf("unexpected expression: %#v", v)
	}

	return nil
}

func processSelectorExpr(exin *ast.SelectorExpr, w *[]string) (err error) {
	ex := exin
	for {
		*w = append(*w, ex.Sel.Name)
		switch v := ex.X.(type) {
		case *ast.SelectorExpr:
			ex = v
		case *ast.Ident:
			*w = append(*w, v.Name)
			return nil
		default:
			return fmt.Errorf("unexpected expression: %#v", v)
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
		return nil, fmt.Errorf("parse error: %s %s", src, err)
	}
	glog.V(2).Infof("%s : %s", src, a)
	ex = &Expression{}
	if err = process(a, ex); err != nil {
		return nil, err
	}
	return ex, nil
}

// Evaluator for a c-like expression language.
type cexl struct {
	//TODO add ast cache
	// function Map
	fMap map[string]Func
}

func (e *cexl) Eval(s string, attrs attribute.Bag) (ret interface{}, err error) {
	var ex *Expression
	// TODO check ast cache
	if ex, err = Parse(s); err != nil {
		return
	}
	return ex.Eval(attrs, e.fMap)
}

// Eval evaluates given expression using the attribute bag to a string
func (e *cexl) EvalString(s string, attrs attribute.Bag) (ret string, err error) {
	var uret interface{}
	var ok bool
	if uret, err = e.Eval(s, attrs); err != nil {
		return
	}
	if ret, ok = uret.(string); ok {
		return
	}
	return "", fmt.Errorf("typeError: got %s, expected string", reflect.TypeOf(uret).String())
}

func (e *cexl) EvalPredicate(s string, attrs attribute.Bag) (ret bool, err error) {
	var uret interface{}
	var ok bool
	if uret, err = e.Eval(s, attrs); err != nil {
		return
	}
	if ret, ok = uret.(bool); ok {
		return
	}
	return false, fmt.Errorf("typeError: got %s, expected bool", reflect.TypeOf(uret).String())
}

// Validate validates expression for syntactic correctness.
// TODO check if all functions and attributes in the expression are defined.
// at present this violates the contract with Func.Call that ensures
// arity and arg types. It is upto the policy author to write correct policies.
func (e *cexl) Validate(s string) (err error) {
	var ex *Expression
	if ex, err = Parse(s); err != nil {
		return err
	}
	// TODO call ex.TypeCheck() when vocabulary is available

	glog.V(2).Infof("%s --> %s", s, ex)
	return nil
}

// NewCEXLEvaluator returns a new Evaluator of this type.
func NewCEXLEvaluator() Evaluator {
	return &cexl{
		fMap: FuncMap(),
	}
}
