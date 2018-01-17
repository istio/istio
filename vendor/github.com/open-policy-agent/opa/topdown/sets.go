// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import "github.com/open-policy-agent/opa/ast"
import "github.com/open-policy-agent/opa/topdown/builtins"

// Deprecated in v0.4.2 in favour of minus/infix "-" operation.
func builtinSetDiff(a, b ast.Value) (ast.Value, error) {

	s1, err := builtins.SetOperand(a, 1)
	if err != nil {
		return nil, err
	}

	s2, err := builtins.SetOperand(b, 2)
	if err != nil {
		return nil, err
	}

	return s1.Diff(s2), nil
}

func init() {
	RegisterFunctionalBuiltin2(ast.SetDiff.Name, builtinSetDiff)
}
