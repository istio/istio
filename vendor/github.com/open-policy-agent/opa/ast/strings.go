// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"reflect"
	"strings"
)

// TypeName returns a human readable name for the AST element type.
func TypeName(x interface{}) string {
	return strings.ToLower(reflect.Indirect(reflect.ValueOf(x)).Type().Name())
}

// The type names provide consistent strings for types in error messages.
const (
	NullTypeName               = "null"
	BooleanTypeName            = "boolean"
	StringTypeName             = "string"
	NumberTypeName             = "number"
	VarTypeName                = "var"
	RefTypeName                = "ref"
	ArrayTypeName              = "array"
	ObjectTypeName             = "object"
	SetTypeName                = "set"
	ArrayComprehensionTypeName = "arraycomprehension"
	ExprTypeName               = "expr"
	WithTypeName               = "with"
	BodyTypeName               = "body"
	HeadTypeName               = "head"
	RuleTypeName               = "rule"
	ArgsTypeName               = "args"
	ImportTypeName             = "import"
	PackageTypeName            = "package"
)
