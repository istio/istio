// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cel

import (
	"fmt"

	"github.com/google/cel-go/common"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// CheckedExprToAst converts a checked expression proto message to an Ast.
func CheckedExprToAst(checkedExpr *exprpb.CheckedExpr) Ast {
	return &astValue{
		expr:    checkedExpr.GetExpr(),
		info:    checkedExpr.GetSourceInfo(),
		source:  common.NewInfoSource(checkedExpr.GetSourceInfo()),
		refMap:  checkedExpr.GetReferenceMap(),
		typeMap: checkedExpr.GetTypeMap(),
	}
}

// AstToCheckedExpr converts an Ast to an protobuf CheckedExpr value.
//
// If the Ast.IsChecked() returns false, this conversion method will return an error.
func AstToCheckedExpr(a Ast) (*exprpb.CheckedExpr, error) {
	if !a.IsChecked() {
		return nil, fmt.Errorf("cannot convert unchecked ast")
	}
	return &exprpb.CheckedExpr{
		Expr:         a.Expr(),
		SourceInfo:   a.SourceInfo(),
		ReferenceMap: a.(*astValue).refMap,
		TypeMap:      a.(*astValue).typeMap,
	}, nil
}

// ParsedExprToAst converts a parsed expression proto message to an Ast.
func ParsedExprToAst(parsedExpr *exprpb.ParsedExpr) Ast {
	return &astValue{
		expr:   parsedExpr.GetExpr(),
		info:   parsedExpr.GetSourceInfo(),
		source: common.NewInfoSource(parsedExpr.GetSourceInfo()),
	}
}

// AstToParsedExpr converts an Ast to an protobuf ParsedExpr value.
func AstToParsedExpr(a Ast) (*exprpb.ParsedExpr, error) {
	return &exprpb.ParsedExpr{
		Expr:       a.Expr(),
		SourceInfo: a.SourceInfo(),
	}, nil
}
