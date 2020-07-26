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

package cel

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"time"

	"golang.org/x/tools/go/ast/astutil"
)

// rewrite the AST to eliminate two problematic sugars:
// - "|" operator turns into flat (_, _) macro (note that this is applied recursively, e.g.
//   (a | b) | c is turned into (a, b, c) expansion
// - "1s" duration string turns into duration("1s") explicit conversion call
func rewriteCEXL(cursor *astutil.Cursor) bool {
	switch n := cursor.Node().(type) {
	case *ast.BinaryExpr:
		if n.Op != token.OR {
			break
		}

		args := make([]ast.Expr, 0, 2)
		if left, ok := n.X.(*ast.CallExpr); ok {
			if lf, ok := left.Fun.(*ast.Ident); ok && lf.Name == pick {
				args = append(args, left.Args...)
			} else {
				args = append(args, n.X)
			}
		} else {
			args = append(args, n.X)
		}

		if right, ok := n.Y.(*ast.CallExpr); ok {
			if rf, ok := right.Fun.(*ast.Ident); ok && rf.Name == pick {
				args = append(args, right.Args...)
			} else {
				args = append(args, n.Y)
			}
		} else {
			args = append(args, n.Y)
		}
		cursor.Replace(&ast.CallExpr{
			Fun:  &ast.Ident{Name: pick},
			Args: args,
		})

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
