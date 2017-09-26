// Copyright 2017 Istio Authors
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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	lru "github.com/hashicorp/golang-lru"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/attribute"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/pool"
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

// AttributeDescriptorFinder finds attribute descriptors.
type AttributeDescriptorFinder interface {
	// GetAttribute finds attribute descriptor in the vocabulary. returns nil if not found.
	GetAttribute(name string) *cfgpb.AttributeManifest_AttributeInfo
}

// EvalType Function an expression using fMap and attribute vocabulary. Returns the type that this expression evaluates to.
func (e *Expression) EvalType(attrs AttributeDescriptorFinder, fMap map[string]FuncBase) (valueType dpb.ValueType, err error) {
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

// Eval returns value of the contained variable or error
func (v *Variable) Eval(attrs attribute.Bag) (interface{}, error) {
	if val, ok := attrs.Get(v.Name); ok {
		return val, nil
	}
	return nil, fmt.Errorf("unresolved attribute %s", v.Name)
}

// Eval evaluates the expression given an attribute bag and a function map.
func (e *Expression) Eval(attrs attribute.Bag, fMap map[string]FuncBase) (interface{}, error) {
	if e.Const != nil {
		return e.Const.Value, nil
	}
	if e.Var != nil {
		return e.Var.Eval(attrs)
	}

	fn := fMap[e.Fn.Name]
	if fn == nil {
		return nil, fmt.Errorf("unknown function: %s", e.Fn.Name)
	}
	// may panic
	return fn.(Func).Call(attrs, e.Fn.Args, fMap)
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
	Name string
	Args []*Expression
}

func (f *Function) String() string {
	w := pool.GetBuffer()
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
func (f *Function) EvalType(attrs AttributeDescriptorFinder, fMap map[string]FuncBase) (valueType dpb.ValueType, err error) {
	fn := fMap[f.Name]
	if fn == nil {
		return valueType, fmt.Errorf("unknown function: %s", f.Name)
	}

	var idx int
	argTypes := fn.ArgTypes()

	if len(f.Args) < len(argTypes) {
		return valueType, fmt.Errorf("%s arity mismatch. Got %d arg(s), expected %d arg(s)", f, len(f.Args), len(argTypes))
	}

	var argType dpb.ValueType
	tmplType := dpb.VALUE_TYPE_UNSPECIFIED
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

	retType := fn.ReturnType()
	if retType == dpb.VALUE_TYPE_UNSPECIFIED {
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
			tgt.Const = &Constant{StrValue: lv, Type: dpb.BOOL, Value: typedVal}
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
		ww := pool.GetBuffer()
		ww.WriteString(w[len(w)-1])
		for idx := len(w) - 2; idx >= 0; idx-- {
			ww.WriteString("." + w[idx])
		}
		tgt.Var = &Variable{Name: ww.String()}
		pool.PutBuffer(ww)
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
		return nil, fmt.Errorf("unable to parse expression '%s': %v", src, err)
	}
	if glog.V(6) {
		glog.Infof("Parsed expression '%s' into '%v'", src, a)
	}

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

// DefaultCacheSize is the default size for the expression cache.
const DefaultCacheSize = 1024

// Evaluator for a c-like expression language.
type cexl struct {
	cache *lru.Cache
	// function Map
	fMap map[string]FuncBase
}

func (e *cexl) cacheGetExpression(exprStr string) (ex *Expression, err error) {

	// TODO: add normalization for exprStr string, so that 'a | b' is same as 'a|b', and  'a == b' is same as 'b == a'

	if v, found := e.cache.Get(exprStr); found {
		return v.(*Expression), nil
	}

	if glog.V(6) {
		glog.Infof("expression cache miss for '%s'", exprStr)
	}

	ex, err = Parse(exprStr)
	if err != nil {
		return nil, err
	}
	if glog.V(6) {
		glog.Infof("caching expression for '%s''", exprStr)
	}

	_ = e.cache.Add(exprStr, ex)
	return ex, nil
}

func (e *cexl) Eval(s string, attrs attribute.Bag) (ret interface{}, err error) {
	var ex *Expression
	if ex, err = e.cacheGetExpression(s); err != nil {
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

func (e *cexl) EvalType(expr string, attrFinder AttributeDescriptorFinder) (dpb.ValueType, error) {
	v, err := e.cacheGetExpression(expr)
	if err != nil {
		return dpb.VALUE_TYPE_UNSPECIFIED, fmt.Errorf("failed to parse expression '%s': %v", expr, err)
	}
	return v.EvalType(attrFinder, e.fMap)
}

func (e *cexl) AssertType(expr string, finder AttributeDescriptorFinder, expectedType dpb.ValueType) error {
	if t, err := e.EvalType(expr, finder); err != nil {
		return err
	} else if t != expectedType {
		return fmt.Errorf("expression '%s' evaluated to type %v, expected type %v", expr, t, expectedType)
	}
	return nil
}

// NewCEXLEvaluator returns a new Evaluator of this type.
func NewCEXLEvaluator(cacheSize int) (Evaluator, error) {
	cache, err := lru.New(cacheSize)
	if err != nil {
		return nil, err
	}
	return &cexl{
		fMap: FuncMap(), cache: cache,
	}, nil
}
