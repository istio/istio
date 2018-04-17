// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"github.com/open-policy-agent/opa/ast"
)

func evalWalk(bctx BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {
	input := args[0]
	filter := getOutputPath(args)
	var path ast.Array
	return walk(filter, path, input, iter)
}

func walk(filter, path ast.Array, input *ast.Term, iter func(*ast.Term) error) error {

	if len(filter) == 0 {
		if err := iter(ast.ArrayTerm(ast.NewTerm(path), input)); err != nil {
			return err
		}
	}

	if len(filter) > 0 {
		key := filter[0]
		filter = filter[1:]
		if key.IsGround() {
			if term := input.Get(key); term != nil {
				return walk(filter, append(path, key), term, iter)
			}
			return nil
		}
	}

	switch v := input.Value.(type) {
	case ast.Array:
		for i := range v {
			path = append(path, ast.IntNumberTerm(i))
			if err := walk(filter, path, v[i], iter); err != nil {
				return err
			}
			path = path[:len(path)-1]
		}
	case ast.Object:
		return v.Iter(func(k, v *ast.Term) error {
			path = append(path, k)
			if err := walk(filter, path, v, iter); err != nil {
				return err
			}
			path = path[:len(path)-1]
			return nil
		})
	case ast.Set:
		return v.Iter(func(elem *ast.Term) error {
			path = append(path, elem)
			if err := walk(filter, path, elem, iter); err != nil {
				return err
			}
			path = path[:len(path)-1]
			return nil
		})
	}

	return nil
}

func getOutputPath(args []*ast.Term) ast.Array {
	if len(args) == 2 {
		if arr, ok := args[1].Value.(ast.Array); ok {
			if len(arr) == 2 {
				if path, ok := arr[0].Value.(ast.Array); ok {
					return path
				}
			}
		}
	}
	return nil
}

func init() {
	RegisterBuiltinFunc(ast.WalkBuiltin.Name, evalWalk)
}
