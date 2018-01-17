// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import "github.com/open-policy-agent/opa/ast"

func evalWalk(bctx BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {
	input := args[0]
	var path ast.Array
	return walk(input, path, iter)
}

func walk(input *ast.Term, path ast.Array, iter func(*ast.Term) error) error {

	output := ast.ArrayTerm(ast.NewTerm(path), input)

	if err := iter(output); err != nil {
		return err
	}

	switch v := input.Value.(type) {
	case ast.Array:
		for i := range v {
			path = append(path, ast.IntNumberTerm(i))
			if err := walk(v[i], path, iter); err != nil {
				return err
			}
			path = path[:len(path)-1]
		}
	case ast.Object:
		for _, pair := range v {
			path = append(path, pair[0])
			if err := walk(pair[1], path, iter); err != nil {
				return err
			}
			path = path[:len(path)-1]
		}
	case *ast.Set:
		var err error
		v.Iter(func(elem *ast.Term) bool {
			if err != nil {
				return true
			}
			path = append(path, elem)
			if err = walk(elem, path, iter); err != nil {
				return true
			}
			path = path[:len(path)-1]
			return false
		})
		return err
	}

	return nil
}

func init() {
	RegisterBuiltinFunc(ast.WalkBuiltin.Name, evalWalk)
}
