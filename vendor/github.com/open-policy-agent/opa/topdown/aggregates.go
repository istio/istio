// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"math/big"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func builtinCount(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Array:
		return ast.IntNumberTerm(len(a)).Value, nil
	case ast.Object:
		return ast.IntNumberTerm(a.Len()).Value, nil
	case ast.Set:
		return ast.IntNumberTerm(a.Len()).Value, nil
	case ast.String:
		return ast.IntNumberTerm(len(a)).Value, nil
	}
	return nil, builtins.NewOperandTypeErr(1, a, "array", "object", "set")
}

func builtinSum(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Array:
		sum := big.NewFloat(0)
		for _, x := range a {
			n, ok := x.Value.(ast.Number)
			if !ok {
				return nil, builtins.NewOperandElementErr(1, a, x.Value, "number")
			}
			sum = new(big.Float).Add(sum, builtins.NumberToFloat(n))
		}
		return builtins.FloatToNumber(sum), nil
	case ast.Set:
		sum := big.NewFloat(0)
		err := a.Iter(func(x *ast.Term) error {
			n, ok := x.Value.(ast.Number)
			if !ok {
				return builtins.NewOperandElementErr(1, a, x.Value, "number")
			}
			sum = new(big.Float).Add(sum, builtins.NumberToFloat(n))
			return nil
		})
		return builtins.FloatToNumber(sum), err
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinProduct(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Array:
		product := big.NewFloat(1)
		for _, x := range a {
			n, ok := x.Value.(ast.Number)
			if !ok {
				return nil, builtins.NewOperandElementErr(1, a, x.Value, "number")
			}
			product = new(big.Float).Mul(product, builtins.NumberToFloat(n))
		}
		return builtins.FloatToNumber(product), nil
	case ast.Set:
		product := big.NewFloat(1)
		err := a.Iter(func(x *ast.Term) error {
			n, ok := x.Value.(ast.Number)
			if !ok {
				return builtins.NewOperandElementErr(1, a, x.Value, "number")
			}
			product = new(big.Float).Mul(product, builtins.NumberToFloat(n))
			return nil
		})
		return builtins.FloatToNumber(product), err
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinMax(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Array:
		if len(a) == 0 {
			return nil, BuiltinEmpty{}
		}
		var max ast.Value = ast.Null{}
		for i := range a {
			if ast.Compare(max, a[i].Value) <= 0 {
				max = a[i].Value
			}
		}
		return max, nil
	case ast.Set:
		if a.Len() == 0 {
			return nil, BuiltinEmpty{}
		}
		max, err := a.Reduce(ast.NullTerm(), func(max *ast.Term, elem *ast.Term) (*ast.Term, error) {
			if ast.Compare(max, elem) <= 0 {
				return elem, nil
			}
			return max, nil
		})
		return max.Value, err
	}

	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinMin(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Array:
		if len(a) == 0 {
			return nil, BuiltinEmpty{}
		}
		min := a[0].Value
		for i := range a {
			if ast.Compare(min, a[i].Value) >= 0 {
				min = a[i].Value
			}
		}
		return min, nil
	case ast.Set:
		if a.Len() == 0 {
			return nil, BuiltinEmpty{}
		}
		min, err := a.Reduce(ast.NullTerm(), func(min *ast.Term, elem *ast.Term) (*ast.Term, error) {
			// The null term is considered to be less than any other term,
			// so in order for min of a set to make sense, we need to check
			// for it.
			if min.Value.Compare(ast.Null{}) == 0 {
				return elem, nil
			}

			if ast.Compare(min, elem) >= 0 {
				return elem, nil
			}
			return min, nil
		})
		return min.Value, err
	}

	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func builtinSort(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Array:
		return a.Sorted(), nil
	case ast.Set:
		return a.Sorted(), nil
	}
	return nil, builtins.NewOperandTypeErr(1, a, "set", "array")
}

func init() {
	RegisterFunctionalBuiltin1(ast.Count.Name, builtinCount)
	RegisterFunctionalBuiltin1(ast.Sum.Name, builtinSum)
	RegisterFunctionalBuiltin1(ast.Product.Name, builtinProduct)
	RegisterFunctionalBuiltin1(ast.Max.Name, builtinMax)
	RegisterFunctionalBuiltin1(ast.Min.Name, builtinMin)
	RegisterFunctionalBuiltin1(ast.Sort.Name, builtinSort)
}
