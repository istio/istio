// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"strconv"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func builtinToNumber(a ast.Value) (ast.Value, error) {
	switch a := a.(type) {
	case ast.Null:
		return ast.Number("0"), nil
	case ast.Boolean:
		if a {
			return ast.Number("1"), nil
		}
		return ast.Number("0"), nil
	case ast.Number:
		return a, nil
	case ast.String:
		_, err := strconv.ParseFloat(string(a), 64)
		if err != nil {
			return nil, err
		}
		return ast.Number(a), nil
	}
	return nil, builtins.NewOperandTypeErr(1, a, "null", "boolean", "number", "string")
}

func init() {
	RegisterFunctionalBuiltin1(ast.ToNumber.Name, builtinToNumber)
}
