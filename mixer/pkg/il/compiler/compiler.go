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

// Package compiler implements a compiler that converts Mixer's expression language into a
// Mixer IL-based program that can be executed via an interpreter.
package compiler

import (
	"fmt"

	"github.com/golang/glog"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/il"
)

type generator struct {
	program *il.Program
	builder *il.Builder
	finder  expr.AttributeDescriptorFinder
	err     error
}

// nilMode is an enum flag for specifying how the emitted code should be handling potential nils.
type nilMode int

const (
	// nmNone indicates that the emitted code should assume the expression will not yield any nils.
	// If a nil is encountered, then it will be treated as an error. The result of the expression
	// will be on top of the stack.
	nmNone nilMode = iota

	// nmJmpOnValue indicates that the emitted code should immediately jump to the label (that is
	// supplied on the side) when it resolves the expression to a non-nil value. If the value is nil,
	// the emitted code will "fallthrough" and allow the immediately-following piece of code to handle
	// the nil  case.
	nmJmpOnValue
)

// Result is returned as the result of compilation.
type Result struct {
	Program    *il.Program
	Expression *expr.Expression
}

// Compile converts the given expression text, into an IL based program.
func Compile(text string, finder expr.AttributeDescriptorFinder) (Result, error) {
	p := il.NewProgram()

	expression, err := expr.Parse(text)
	if err != nil {
		return Result{}, err
	}

	exprType, err := expression.EvalType(finder, expr.FuncMap())
	if err != nil {
		return Result{}, err
	}

	g := generator{
		program: p,
		builder: il.NewBuilder(p.Strings()),
		finder:  finder,
	}

	returnType := g.toIlType(exprType)
	g.generate(expression, 0, nmNone, "")
	if g.err != nil {
		glog.Warningf("compiler.Compile failed. expr:'%s', err:'%v'", text, g.err)
		return Result{}, g.err
	}

	g.builder.Ret()
	body := g.builder.Build()
	if err = g.program.AddFunction("eval", []il.Type{}, returnType, body); err != nil {
		g.internalError(err.Error())
		return Result{}, err
	}

	return Result{
		Program:    p,
		Expression: expression,
	}, nil
}

func (g *generator) toIlType(t dpb.ValueType) il.Type {
	switch t {
	case dpb.STRING:
		return il.String
	case dpb.BOOL:
		return il.Bool
	case dpb.INT64:
		return il.Integer
	case dpb.DOUBLE:
		return il.Double
	case dpb.STRING_MAP:
		return il.StringMap
	default:
		g.internalError("unhandled expression type: '%v'", t)
		return il.Unknown
	}
}

func (g *generator) evalType(e *expr.Expression) il.Type {
	dvt, _ := e.EvalType(g.finder, expr.FuncMap())
	return g.toIlType(dvt)
}

func (g *generator) generate(e *expr.Expression, depth int, mode nilMode, valueJmpLabel string) {
	switch {
	case e.Const != nil:
		g.generateConstant(e.Const)
	case e.Var != nil:
		g.generateVariable(e.Var, mode, valueJmpLabel)
	case e.Fn != nil:
		g.generateFunction(e.Fn, depth, mode, valueJmpLabel)
	default:
		g.internalError("unexpected expression type encountered.")
	}
}

func (g *generator) generateVariable(v *expr.Variable, mode nilMode, valueJmpLabel string) {
	i := g.finder.GetAttribute(v.Name)
	ilType := g.toIlType(i.ValueType)
	switch ilType {
	case il.Integer:
		switch mode {
		case nmNone:
			g.builder.ResolveInt(v.Name)
		case nmJmpOnValue:
			g.builder.TResolveInt(v.Name)
			g.builder.Jnz(valueJmpLabel)
		}

	case il.String:
		switch mode {
		case nmNone:
			g.builder.ResolveString(v.Name)
		case nmJmpOnValue:
			g.builder.TResolveString(v.Name)
			g.builder.Jnz(valueJmpLabel)
		}

	case il.Bool:
		switch mode {
		case nmNone:
			g.builder.ResolveBool(v.Name)
		case nmJmpOnValue:
			g.builder.TResolveBool(v.Name)
			g.builder.Jnz(valueJmpLabel)
		}

	case il.Double:
		switch mode {
		case nmNone:
			g.builder.ResolveDouble(v.Name)
		case nmJmpOnValue:
			g.builder.TResolveDouble(v.Name)
			g.builder.Jnz(valueJmpLabel)
		}

	case il.StringMap:
		switch mode {
		case nmNone:
			g.builder.ResolveMap(v.Name)
		case nmJmpOnValue:
			g.builder.TResolveMap(v.Name)
			g.builder.Jnz(valueJmpLabel)
		}

	default:
		g.internalError("unrecognized variable type: '%v'", i.ValueType)
	}
}

func (g *generator) generateFunction(f *expr.Function, depth int, mode nilMode, valueJmpLabel string) {

	switch f.Name {
	case "EQ":
		g.generateEq(f, depth)
	case "NEQ":
		g.generateNeq(f, depth)
	case "LOR":
		g.generateLor(f, depth)
	case "LAND":
		g.generateLand(f, depth)
	case "INDEX":
		g.generateIndex(f, depth, mode, valueJmpLabel)
	case "OR":
		g.generateOr(f, depth, mode, valueJmpLabel)
	default:
		g.internalError("function not yet implemented: %s", f.Name)
	}
}

func (g *generator) generateEq(f *expr.Function, depth int) {
	exprType := g.evalType(f.Args[0])
	g.generate(f.Args[0], depth+1, nmNone, "")

	var constArg1 interface{}
	if f.Args[1].Const != nil {
		constArg1 = f.Args[1].Const.Value
	} else {
		g.generate(f.Args[1], depth+1, nmNone, "")
	}

	switch exprType {
	case il.Bool:
		if constArg1 != nil {
			g.builder.AEQBool(constArg1.(bool))
		} else {
			g.builder.EQBool()
		}

	case il.String:
		if constArg1 != nil {
			g.builder.AEQString(constArg1.(string))
		} else {
			g.builder.EQString()
		}

	case il.Integer:
		if constArg1 != nil {
			g.builder.AEQInteger(constArg1.(int64))
		} else {
			g.builder.EQInteger()
		}

	case il.Double:
		if constArg1 != nil {
			g.builder.AEQDouble(constArg1.(float64))
		} else {
			g.builder.EQDouble()
		}

	default:
		g.internalError("equality for type not yet implemented: %v", exprType)
	}
}

func (g *generator) generateNeq(f *expr.Function, depth int) {
	g.generateEq(f, depth+1)
	g.builder.Not()
}

func (g *generator) generateLor(f *expr.Function, depth int) {
	g.generate(f.Args[0], depth+1, nmNone, "")
	lr := g.builder.AllocateLabel()
	le := g.builder.AllocateLabel()
	g.builder.Jz(lr)
	g.builder.APushBool(true)
	if depth == 0 {
		g.builder.Ret()
	} else {
		g.builder.Jmp(le)
	}
	g.builder.SetLabelPos(lr)
	g.generate(f.Args[1], depth+1, nmNone, "")

	if depth != 0 {
		g.builder.SetLabelPos(le)
	}
}

func (g *generator) generateLand(f *expr.Function, depth int) {
	for _, a := range f.Args {
		g.generate(a, depth+1, nmNone, "")
	}

	g.builder.And()
}

func (g *generator) generateIndex(f *expr.Function, depth int, mode nilMode, valueJmpLabel string) {

	switch mode {
	case nmNone:
		// Assume both the indexing target (arg[0]) and the index variable (arg[1]) are non-nil.
		g.generate(f.Args[0], depth+1, nmNone, "")

		if f.Args[1].Const != nil {
			str := f.Args[1].Const.Value.(string)
			g.builder.ALookup(str)
		} else {
			g.generate(f.Args[1], depth+1, nmNone, "")
			g.builder.Lookup()
		}

	case nmJmpOnValue:
		// Assume both the indexing target (arg[0]) and the index variable (arg[1]) can be nil.
		// Stack the operations such that first the target is resolved, if not nil, then the
		// indexer is resolved, and finally if the indexer is not nil, the value is looked up.
		//
		//   As an example, consider the expression (ar["c"] | "foo") and we were supplied "LOuterLabel"
		//   as valueJmpLabel
		//   ...
		//                                     // Begin Args[0] eval (i.e. ar)
		//   tresolve_m "ar"                   // Try to resolve "ar"
		//   jnz LTargetResolved               // If resolved, then jump to LTargetResolved.
		//                                     // End Args[0] eval (i.e. ar)
		//
		//   jmp LEnd                          // This means Args[0] evaluated to nil, so jump to end
		//                                     // and fallthrough.
		// LTargetResolved:
		//                                     // Begin Args[1] eval (i.e. "c")
		//   apush_s "c"                       // Constant is directly evaluated.
		//                                     // End Args[1] eval (i.e. "c")
		//
		//   tlookup                           // With Args[0] and Arg[1] on stack, perform conditional lookup
		//   jnz LOuterLabel                   // If successful, jump to LOuterLabel
		// LEnd:                               // Otherwise fall through with value evaluated to nil.
		//                                     // From here on, evaluate the `... | "foo"` part, as "..." evaluated to nil.
		//   apush_s "foo"
		// LOuterLabel:
		//
		lEnd := g.builder.AllocateLabel()
		lTargetResolved := g.builder.AllocateLabel()
		g.generate(f.Args[0], depth+1, nmJmpOnValue, lTargetResolved)
		g.builder.Jmp(lEnd)
		g.builder.SetLabelPos(lTargetResolved)

		if f.Args[1].Const != nil {
			str := f.Args[1].Const.Value.(string)
			g.builder.APushStr(str)
		} else {
			lArgumentResolved := g.builder.AllocateLabel()
			g.generate(f.Args[1], depth+1, nmJmpOnValue, lArgumentResolved)
			g.builder.Jmp(lEnd)
			g.builder.SetLabelPos(lArgumentResolved)
		}
		g.builder.TLookup()
		g.builder.Jnz(valueJmpLabel)
		g.builder.SetLabelPos(lEnd)
	}
}

func (g *generator) generateOr(f *expr.Function, depth int, mode nilMode, valueJmpLabel string) {
	switch mode {
	case nmNone:
		// If the caller expects non-null result, evaluate Args[1] as non-null, and jump to end if
		// it resolves to a value.
		lEnd := g.builder.AllocateLabel()
		g.generate(f.Args[0], depth+1, nmJmpOnValue, lEnd)

		// Continue calculation of the right part of or, assuming left part evaluated to nil.
		// If this is a chain of "OR"s, we can chain through the ORs (i.e. the inner Or's Arg[0]
		// is evaluated as nmJmpOnValue, this allows short-circuiting all the way to the end.
		if f.Args[1].Fn != nil && f.Args[1].Fn.Name == "OR" {
			g.generate(f.Args[1], depth+1, nmJmpOnValue, lEnd)
		} else {
			g.generate(f.Args[1], depth+1, nmNone, "")
		}
		g.builder.SetLabelPos(lEnd)

	case nmJmpOnValue:
		g.generate(f.Args[0], depth+1, nmJmpOnValue, valueJmpLabel)
		g.generate(f.Args[1], depth+1, nmJmpOnValue, valueJmpLabel)
	}
}

func (g *generator) generateConstant(c *expr.Constant) {
	switch c.Type {
	case dpb.STRING:
		s := c.Value.(string)
		g.builder.APushStr(s)
	case dpb.BOOL:
		b := c.Value.(bool)
		g.builder.APushBool(b)
	case dpb.INT64:
		i := c.Value.(int64)
		g.builder.APushInt(i)
	case dpb.DOUBLE:
		d := c.Value.(float64)
		g.builder.APushDouble(d)
	default:
		g.internalError("unhandled constant type: %v", c.Type)
	}
}

func (g *generator) internalError(format string, args ...interface{}) {
	glog.Warningf("internal compiler error -- "+format, args)
	if g.err == nil {
		g.err = fmt.Errorf("internal compiler error -- %s", fmt.Sprintf(format, args...))
	}
}
