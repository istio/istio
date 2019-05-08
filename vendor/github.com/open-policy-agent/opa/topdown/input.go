// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"

	"github.com/open-policy-agent/opa/ast"
)

var errConflictingInputDoc = fmt.Errorf("conflicting input documents")
var errBadInputPath = fmt.Errorf("bad input document path")

func makeInput(pairs [][2]*ast.Term) (ast.Value, error) {

	// Fast-path for empty case.
	if len(pairs) == 0 {
		return nil, nil
	}

	// Fast-path for the root case.
	if len(pairs) == 1 && pairs[0][0].Value.Compare(ast.InputRootRef) == 0 {
		return pairs[0][1].Value, nil
	}

	var input ast.Value

	for _, pair := range pairs {

		if err := ast.IsValidImportPath(pair[0].Value); err != nil {
			return nil, errBadInputPath
		}

		ref := pair[0].Value.(ast.Ref)

		if len(ref) == 1 {
			if input != nil {
				return nil, errConflictingInputDoc
			}
			input = pair[1].Value
		} else {
			obj := makeTree(ref[1:], pair[1])
			if input == nil {
				input = obj
			} else {
				reqObj, ok := input.(ast.Object)
				if !ok {
					return nil, errConflictingInputDoc
				}
				input, ok = reqObj.Merge(obj)
				if !ok {
					return nil, errConflictingInputDoc
				}
			}

		}
	}

	return input, nil
}

// makeTree returns an object that represents a document where the value v is
// the leaf and elements in k represent intermediate objects.
func makeTree(k ast.Ref, v *ast.Term) ast.Object {
	var obj ast.Object
	for i := len(k) - 1; i >= 1; i-- {
		obj = ast.NewObject(ast.Item(k[i], v))
		v = &ast.Term{Value: obj}
	}
	obj = ast.NewObject(ast.Item(k[0], v))
	return obj
}
