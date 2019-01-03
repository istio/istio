// Copyright 2018 Istio Authors
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

package cel

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/google/cel-go/common/debug"
	"golang.org/x/tools/go/ast/astutil"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	dpb "istio.io/api/policy/v1beta1"
	cexl "istio.io/istio/mixer/pkg/lang/ast"
)

var fMap = map[string]string{
	"_+_": "ADD",
	"_-_": "SUB",
	"_*_": "MUL",
	"_/_": "QUO",
	"_%_": "REM",

	"_&&_": "LAND",
	"_||_": "LOR",

	"_==_": "EQ",
	"_<_":  "LT",
	"_>_":  "GT",
	"!_":   "NOT",

	"_!=_": "NEQ",
	"_<=_": "LEQ",
	"_>=_": "GEQ",

	"_[_]": "INDEX",

	elvis: "OR",
}

func exprCELtoCEXL(in *exprpb.Expr, out *cexl.Expression) error {
	switch v := in.ExprKind.(type) {
	case *exprpb.Expr_ConstExpr:
		out.Const = &cexl.Constant{StrValue: debug.ToDebugString(in)}

		switch c := v.ConstExpr.ConstantKind.(type) {
		case *exprpb.Constant_NullValue:
			return errors.New("null constant")
		case *exprpb.Constant_BoolValue:
			out.Const.Type = dpb.BOOL
			out.Const.Value = c.BoolValue
		case *exprpb.Constant_Int64Value:
			out.Const.Type = dpb.INT64
			out.Const.Value = c.Int64Value
		case *exprpb.Constant_Uint64Value:
			out.Const.Type = dpb.INT64
			out.Const.Value = int64(c.Uint64Value)
		case *exprpb.Constant_DoubleValue:
			out.Const.Type = dpb.DOUBLE
			out.Const.Value = c.DoubleValue
		case *exprpb.Constant_BytesValue:
			return errors.New("bytes value")
		case *exprpb.Constant_DurationValue:
			out.Const.Type = dpb.DURATION
			dur, err := ptypes.Duration(c.DurationValue)
			if err != nil {
				return err
			}
			out.Const.Value = dur
		case *exprpb.Constant_TimestampValue:
			out.Const.Type = dpb.TIMESTAMP
			ts, err := ptypes.Timestamp(c.TimestampValue)
			if err != nil {
				return err
			}
			out.Const.Value = ts
		case *exprpb.Constant_StringValue:
			out.Const.Type = dpb.STRING
			out.Const.Value = c.StringValue
		default:
			return fmt.Errorf("unexpected constant: %s", debug.ToDebugString(in))
		}

	case *exprpb.Expr_IdentExpr:
		out.Var = &cexl.Variable{Name: v.IdentExpr.Name}

	case *exprpb.Expr_CallExpr:
		// partial evaluation of constants for backwards compatibility
		// below converts: duration("1s") into a literal duration 1s
		if v.CallExpr.Function == "duration" && v.CallExpr.Target == nil && len(v.CallExpr.Args) == 1 {
			if c, ok := v.CallExpr.Args[0].ExprKind.(*exprpb.Expr_ConstExpr); ok {
				if str, ok := c.ConstExpr.ConstantKind.(*exprpb.Constant_StringValue); ok {
					if dur, err := time.ParseDuration(str.StringValue); err == nil {
						out.Const = &cexl.Constant{
							Type:     dpb.DURATION,
							Value:    dur,
							StrValue: debug.ToDebugString(v.CallExpr.Args[0]),
						}
						return nil
					}
				}
			}
		}

		var target *cexl.Expression
		if v.CallExpr.Target != nil {
			target = &cexl.Expression{}
			if err := exprCELtoCEXL(v.CallExpr.Target, target); err != nil {
				return err
			}
		}

		fargs := []*cexl.Expression{}
		for _, arg := range v.CallExpr.Args {
			aex := &cexl.Expression{}
			if err := exprCELtoCEXL(arg, aex); err != nil {
				return err
			}
			fargs = append(fargs, aex)
		}

		name, ok := fMap[v.CallExpr.Function]
		if !ok {
			// fallback to externs
			name = v.CallExpr.Function
		}

		out.Fn = &cexl.Function{
			Target: target,
			Name:   name,
			Args:   fargs,
		}

	case *exprpb.Expr_SelectExpr:
		// flatten ident namespace
		if err := exprCELtoCEXL(v.SelectExpr.Operand, out); err != nil {
			return err
		}

		if out.Var != nil {
			out.Var.Name = out.Var.Name + "." + v.SelectExpr.Field
		} else {
			return fmt.Errorf("unexpected selector: %s operand %#v", debug.ToDebugString(in), out)
		}

	default:
		return fmt.Errorf("unexpected expression: %s", debug.ToDebugString(in))
	}

	return nil
}

// rewrite the AST to eliminate two problematic sugars:
// - "|" operator turns into flat (_, _) macro (note that this is applied recursively, e.g.
//   (a | b) | c is turned into (a, b, c) expansion
// - "1s" duration string turns into duration("1s") explicit conversion call
func rewriteCEXL(cursor *astutil.Cursor) bool {
	switch n := cursor.Node().(type) {
	case *ast.BinaryExpr:
		args := make([]ast.Expr, 0, 2)
		if left, ok := n.X.(*ast.CallExpr); ok {
			if lf, ok := left.Fun.(*ast.Ident); ok && lf.Name == elvis {
				args = append(args, left.Args...)
			} else {
				args = append(args, n.X)
			}
		} else {
			args = append(args, n.X)
		}

		if right, ok := n.Y.(*ast.CallExpr); ok {
			if rf, ok := right.Fun.(*ast.Ident); ok && rf.Name == elvis {
				args = append(args, right.Args...)
			} else {
				args = append(args, n.Y)
			}
		} else {
			args = append(args, n.Y)
		}

		if n.Op == token.OR {
			cursor.Replace(&ast.CallExpr{
				Fun:  &ast.Ident{Name: elvis},
				Args: args,
			})
		}
	case *ast.BasicLit:
		if n.Kind != token.STRING {
			break
		}
		unquoted, err := strconv.Unquote(n.Value)
		if err != nil {
			break
		}
		if _, err = time.ParseDuration(unquoted); err != nil {
			break
		}
		cursor.Replace(&ast.CallExpr{
			Fun:  &ast.Ident{Name: "duration"},
			Args: []ast.Expr{n},
		})
	}
	return true
}

func sourceCEXLToCEL(src string) (string, error) {
	node, err := parser.ParseExpr(src)
	if err != nil {
		return "", fmt.Errorf("unable to parse expression '%s': %v", src, err)
	}

	modified := astutil.Apply(node, nil, rewriteCEXL)

	var buf bytes.Buffer
	if err = format.Node(&buf, token.NewFileSet(), modified); err != nil {
		return "", err
	}

	return buf.String(), nil
}
