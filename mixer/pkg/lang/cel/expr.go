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
	"errors"
	"fmt"

	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/parser"
	"github.com/hashicorp/go-multierror"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

const (
	elvis = "getOrElse"
)

// Parse a CEL expression
func Parse(text string) (ex *exprpb.Expr, err error) {
	source := common.NewTextSource(text)

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during CEL parsing of expression %q", text)
		}
	}()

	parsed, errors := parser.Parse(source)
	if errors != nil && len(errors.GetErrors()) > 0 {
		err = fmt.Errorf("parsing error: %v", errors.ToDisplayString())
		return
	}

	u := &unroller{id: maxID(parsed.Expr)}
	ex = u.unroll(parsed.Expr)
	err = u.err
	return
}

// Check verifies a CEL expressions against an attribute manifest
func Check(ex *exprpb.Expr, env *checker.Env) (checked *exprpb.CheckedExpr, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panic during CEL checking of expression")
		}
	}()

	var errors *common.Errors
	checked, errors = checker.Check(&exprpb.ParsedExpr{Expr: ex}, nil, env)
	if errors != nil && len(errors.GetErrors()) > 0 {
		err = fmt.Errorf("type checking error: %s", errorString(errors))
	}
	return
}

func errorString(errors *common.Errors) string {
	out := ""
	for _, err := range errors.GetErrors() {
		if out != "" {
			out = out + "\n"
		}
		out = out + err.Message
	}
	return out
}

// Max ID computation is borrowed from interpreter/astwalker.go

func maxInt(vals ...int64) int64 {
	var result int64
	for _, val := range vals {
		if val > result {
			result = val
		}
	}
	return result
}

func maxID(node *exprpb.Expr) int64 {
	if node == nil {
		return 0
	}
	currID := node.Id
	switch node.ExprKind.(type) {
	case *exprpb.Expr_SelectExpr:
		return maxInt(currID, maxID(node.GetSelectExpr().Operand))
	case *exprpb.Expr_CallExpr:
		call := node.GetCallExpr()
		currID = maxInt(currID, maxID(call.Target))
		for _, arg := range call.Args {
			currID = maxInt(currID, maxID(arg))
		}
		return currID
	case *exprpb.Expr_ListExpr:
		list := node.GetListExpr()
		for _, elem := range list.Elements {
			currID = maxInt(currID, maxID(elem))
		}
		return currID
	case *exprpb.Expr_StructExpr:
		str := node.GetStructExpr()
		for _, entry := range str.Entries {
			currID = maxInt(currID, entry.Id, maxID(entry.GetMapKey()), maxID(entry.Value))
		}
		return currID
	case *exprpb.Expr_ComprehensionExpr:
		compre := node.GetComprehensionExpr()
		return maxInt(currID,
			maxID(compre.IterRange),
			maxID(compre.AccuInit),
			maxID(compre.LoopCondition),
			maxID(compre.LoopStep),
			maxID(compre.Result))
	default:
		return currID
	}
}

type unroller struct {
	err error
	id  int64
}

func (u *unroller) nextID() int64 {
	u.id++
	return u.id
}

// unroll eliminates Elvis and conditional() macros
// it also replaces emptyStringMap with stringmap({})
func (u *unroller) unroll(in *exprpb.Expr) *exprpb.Expr {
	switch v := in.ExprKind.(type) {
	case *exprpb.Expr_ConstExpr, *exprpb.Expr_IdentExpr:
		// do nothing
	case *exprpb.Expr_ListExpr:
		// recurse
		elements := make([]*exprpb.Expr, len(v.ListExpr.Elements))
		for i, element := range v.ListExpr.Elements {
			elements[i] = u.unroll(element)
		}
		return &exprpb.Expr{
			Id: in.Id,
			ExprKind: &exprpb.Expr_ListExpr{
				ListExpr: &exprpb.Expr_CreateList{
					Elements: elements,
				},
			},
		}
	case *exprpb.Expr_StructExpr:
		// recurse
		entries := make([]*exprpb.Expr_CreateStruct_Entry, len(v.StructExpr.Entries))
		for i, entry := range v.StructExpr.Entries {
			out := &exprpb.Expr_CreateStruct_Entry{
				Id:    entry.Id,
				Value: u.unroll(entry.Value),
			}
			switch k := entry.KeyKind.(type) {
			case *exprpb.Expr_CreateStruct_Entry_FieldKey:
				out.KeyKind = k
			case *exprpb.Expr_CreateStruct_Entry_MapKey:
				out.KeyKind = &exprpb.Expr_CreateStruct_Entry_MapKey{
					MapKey: u.unroll(k.MapKey),
				}
			}
			entries[i] = out
		}
		return &exprpb.Expr{
			Id: in.Id,
			ExprKind: &exprpb.Expr_StructExpr{
				StructExpr: &exprpb.Expr_CreateStruct{
					MessageName: v.StructExpr.MessageName,
					Entries:     entries,
				},
			},
		}

	case *exprpb.Expr_SelectExpr:
		// recurse
		return &exprpb.Expr{
			Id: in.Id,
			ExprKind: &exprpb.Expr_SelectExpr{
				SelectExpr: &exprpb.Expr_Select{
					Operand:  u.unroll(v.SelectExpr.Operand),
					Field:    v.SelectExpr.Field,
					TestOnly: v.SelectExpr.TestOnly,
				},
			},
		}
	case *exprpb.Expr_CallExpr:
		// recurse
		var target *exprpb.Expr
		if v.CallExpr.Target != nil {
			target = u.unroll(v.CallExpr.Target)
		}

		args := make([]*exprpb.Expr, len(v.CallExpr.Args))
		for i, arg := range v.CallExpr.Args {
			args[i] = u.unroll(arg)
		}

		// rewrite functions, recurse otherwise
		switch v.CallExpr.Function {
		case "emptyStringMap":
			if target != nil || len(args) > 0 {
				u.err = multierror.Append(u.err, errors.New("unexpected arguments in emptyStringMap()"))
				break
			}
			return &exprpb.Expr{
				Id:       in.Id,
				ExprKind: &exprpb.Expr_StructExpr{StructExpr: &exprpb.Expr_CreateStruct{}},
			}
		case "conditional":
			return &exprpb.Expr{
				Id: in.Id,
				ExprKind: &exprpb.Expr_CallExpr{
					CallExpr: &exprpb.Expr_Call{
						Function: operators.Conditional,
						Target:   target,
						Args:     args,
					},
				},
			}
		case elvis:
			if target != nil {
				u.err = multierror.Append(u.err, fmt.Errorf("unexpected target in expression %q", v))
				break
			}

			// step through arguments to construct a conditional chain
			out := args[len(args)-1]
			var selector *exprpb.Expr
			for i := len(args) - 2; i >= 0; i-- {
				selector = nil
				switch lhs := args[i].ExprKind.(type) {
				case *exprpb.Expr_SelectExpr:
					if !lhs.SelectExpr.TestOnly {
						// a.f | x --> has(a.f) ? a.f : x
						selector = &exprpb.Expr{
							Id: u.nextID(),
							ExprKind: &exprpb.Expr_SelectExpr{
								SelectExpr: &exprpb.Expr_Select{
									Operand:  lhs.SelectExpr.Operand,
									Field:    lhs.SelectExpr.Field,
									TestOnly: true,
								},
							},
						}
					}

				case *exprpb.Expr_CallExpr:
					if lhs.CallExpr.Function == operators.Index {
						// a["f"] | x --> "f" in a ? a["f"] : x
						selector = &exprpb.Expr{
							Id: u.nextID(),
							ExprKind: &exprpb.Expr_CallExpr{
								CallExpr: &exprpb.Expr_Call{
									Function: operators.In,
									Args:     []*exprpb.Expr{lhs.CallExpr.Args[1], lhs.CallExpr.Args[0]},
								},
							},
						}
					}
				}

				// otherwise, a | b --> a
				if selector == nil {
					out = args[i]
				} else {
					out = &exprpb.Expr{
						Id: u.nextID(),
						ExprKind: &exprpb.Expr_CallExpr{
							CallExpr: &exprpb.Expr_Call{
								Function: operators.Conditional,
								Args:     []*exprpb.Expr{selector, args[i], out},
							},
						},
					}
				}
			}
			return out

		default:
			return &exprpb.Expr{
				Id: in.Id,
				ExprKind: &exprpb.Expr_CallExpr{
					CallExpr: &exprpb.Expr_Call{
						Function: v.CallExpr.Function,
						Target:   target,
						Args:     args,
					},
				},
			}
		}

	default:
		u.err = multierror.Append(u.err, fmt.Errorf("unsupported expression kind %q", v))
	}
	return in
}
