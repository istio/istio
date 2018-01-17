// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"strings"

	"github.com/open-policy-agent/opa/types"
)

// Builtins is the registry of built-in functions supported by OPA.
// Call RegisterBuiltin to add a new built-in.
var Builtins []*Builtin

// RegisterBuiltin adds a new built-in function to the registry.
func RegisterBuiltin(b *Builtin) {
	Builtins = append(Builtins, b)
	BuiltinMap[b.Name] = b
	if len(b.Infix) > 0 {
		BuiltinMap[b.Infix] = b
	}
}

// DefaultBuiltins is the registry of built-in functions supported in OPA
// by default. When adding a new built-in function to OPA, update this
// list.
var DefaultBuiltins = [...]*Builtin{
	// =
	Equality,

	// Comparisons
	GreaterThan, GreaterThanEq, LessThan, LessThanEq, NotEqual,

	// Arithmetic
	Plus, Minus, Multiply, Divide, Round, Abs,

	// Binary
	And, Or,

	// Aggregates
	Count, Sum, Product, Max, Min,

	// Casting
	ToNumber,

	// Regular Expressions
	RegexMatch,

	// Sets
	SetDiff,

	// Strings
	Concat, FormatInt, IndexOf, Substring, Lower, Upper, Contains, StartsWith, EndsWith, Split, Replace, Trim, Sprintf,

	// Encoding
	JSONMarshal, JSONUnmarshal, Base64UrlEncode, Base64UrlDecode, YAMLMarshal, YAMLUnmarshal,

	// Tokens
	JWTDecode,

	// Time
	NowNanos, ParseNanos, ParseRFC3339Nanos, ParseDurationNanos,

	// Graphs
	WalkBuiltin,
}

// BuiltinMap provides a convenient mapping of built-in names to
// built-in definitions.
var BuiltinMap map[string]*Builtin

/**
 * Unification
 */

// Equality represents the "=" operator.
var Equality = &Builtin{
	Name:  "eq",
	Infix: "=",
	Decl: types.NewFunction(
		types.Args(types.A, types.A),
		types.T,
	),
}

/**
 * Comparisons
 */

// GreaterThan represents the ">" comparison operator.
var GreaterThan = &Builtin{
	Name:  "gt",
	Infix: ">",
	Decl: types.NewFunction(
		types.Args(types.A, types.A),
		types.T,
	),
}

// GreaterThanEq represents the ">=" comparison operator.
var GreaterThanEq = &Builtin{
	Name:  "gte",
	Infix: ">=",
	Decl: types.NewFunction(
		types.Args(types.A, types.A),
		types.T,
	),
}

// LessThan represents the "<" comparison operator.
var LessThan = &Builtin{
	Name:  "lt",
	Infix: "<",
	Decl: types.NewFunction(
		types.Args(types.A, types.A),
		types.T,
	),
}

// LessThanEq represents the "<=" comparison operator.
var LessThanEq = &Builtin{
	Name:  "lte",
	Infix: "<=",
	Decl: types.NewFunction(
		types.Args(types.A, types.A),
		types.T,
	),
}

// NotEqual represents the "!=" comparison operator.
var NotEqual = &Builtin{
	Name:  "neq",
	Infix: "!=",
	Decl: types.NewFunction(
		types.Args(types.A, types.A),
		types.T,
	),
}

/**
 * Arithmetic
 */

// Plus adds two numbers together.
var Plus = &Builtin{
	Name:  "plus",
	Infix: "+",
	Decl: types.NewFunction(
		types.Args(types.N, types.N),
		types.N,
	),
}

// Minus subtracts the second number from the first number or computes the diff
// between two sets.
var Minus = &Builtin{
	Name:  "minus",
	Infix: "-",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(types.N, types.NewSet(types.A)),
			types.NewAny(types.N, types.NewSet(types.A)),
		),
		types.NewAny(types.N, types.NewSet(types.A)),
	),
}

// Multiply multiplies two numbers together.
var Multiply = &Builtin{
	Name:  "mul",
	Infix: "*",
	Decl: types.NewFunction(
		types.Args(types.N, types.N),
		types.N,
	),
}

// Divide divides the first number by the second number.
var Divide = &Builtin{
	Name:  "div",
	Infix: "/",
	Decl: types.NewFunction(
		types.Args(types.N, types.N),
		types.N,
	),
}

// Round rounds the number up to the nearest integer.
var Round = &Builtin{
	Name: "round",
	Decl: types.NewFunction(
		types.Args(types.N),
		types.N,
	),
}

// Abs returns the number without its sign.
var Abs = &Builtin{
	Name: "abs",
	Decl: types.NewFunction(
		types.Args(types.N),
		types.N,
	),
}

/**
 * Binary
 */

// TODO(tsandall): update binary operators to support integers.

// And performs an intersection operation on sets.
var And = &Builtin{
	Name:  "and",
	Infix: "&",
	Decl: types.NewFunction(
		types.Args(
			types.NewSet(types.A),
			types.NewSet(types.A),
		),
		types.NewSet(types.A),
	),
}

// Or performs a union operation on sets.
var Or = &Builtin{
	Name:  "or",
	Infix: "|",
	Decl: types.NewFunction(
		types.Args(
			types.NewSet(types.A),
			types.NewSet(types.A),
		),
		types.NewSet(types.A),
	),
}

/**
 * Aggregates
 */

// Count takes a collection or string and counts the number of elements in it.
var Count = &Builtin{
	Name: "count",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(
				types.NewSet(types.A),
				types.NewArray(nil, types.A),
				types.NewObject(nil, types.NewDynamicProperty(types.A, types.A)),
				types.S,
			),
		),
		types.N,
	),
}

// Sum takes an array or set of numbers and sums them.
var Sum = &Builtin{
	Name: "sum",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(
				types.NewSet(types.N),
				types.NewArray(nil, types.N),
			),
		),
		types.N,
	),
}

// Product takes an array or set of numbers and multiplies them.
var Product = &Builtin{
	Name: "product",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(
				types.NewSet(types.N),
				types.NewArray(nil, types.N),
			),
		),
		types.N,
	),
}

// Max returns the maximum value in a collection.
var Max = &Builtin{
	Name: "max",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(
				types.NewSet(types.A),
				types.NewArray(nil, types.A),
			),
		),
		types.A,
	),
}

// Min returns the minimum value in a collection.
var Min = &Builtin{
	Name: "min",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(
				types.NewSet(types.A),
				types.NewArray(nil, types.A),
			),
		),
		types.A,
	),
}

/**
 * Casting
 */

// ToNumber takes a string, bool, or number value and converts it to a number.
// Strings are converted to numbers using strconv.Atoi.
// Boolean false is converted to 0 and boolean true is converted to 1.
var ToNumber = &Builtin{
	Name: "to_number",
	Decl: types.NewFunction(
		types.Args(
			types.NewAny(
				types.N,
				types.S,
				types.B,
				types.NewNull(),
			),
		),
		types.N,
	),
}

/**
 * Regular Expressions
 */

// RegexMatch takes two strings and evaluates to true if the string in the second
// position matches the pattern in the first position.
var RegexMatch = &Builtin{
	Name: "re_match",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.T,
	),
}

/**
 * Strings
 */

// Concat joins an array of strings with an input string.
var Concat = &Builtin{
	Name: "concat",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.NewAny(
				types.NewSet(types.S),
				types.NewArray(nil, types.S),
			),
		),
		types.S,
	),
}

// FormatInt returns the string representation of the number in the given base after converting it to an integer value.
var FormatInt = &Builtin{
	Name: "format_int",
	Decl: types.NewFunction(
		types.Args(
			types.N,
			types.N,
		),
		types.S,
	),
}

// IndexOf returns the index of a substring contained inside a string
var IndexOf = &Builtin{
	Name: "indexof",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.N,
	),
}

// Substring returns the portion of a string for a given start index and a length.
//   If the length is less than zero, then substring returns the remainder of the string.
var Substring = &Builtin{
	Name: "substring",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.N,
			types.N,
		),
		types.S,
	),
}

// Contains returns true if the search string is included in the base string
var Contains = &Builtin{
	Name: "contains",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.T,
	),
}

// StartsWith returns true if the search string begins with the base string
var StartsWith = &Builtin{
	Name: "startswith",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.T,
	),
}

// EndsWith returns true if the search string begins with the base string
var EndsWith = &Builtin{
	Name: "endswith",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.T,
	),
}

// Lower returns the input string but with all characters in lower-case
var Lower = &Builtin{
	Name: "lower",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.S,
	),
}

// Upper returns the input string but with all characters in upper-case
var Upper = &Builtin{
	Name: "upper",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.S,
	),
}

// Split returns an array containing elements of the input string split on a delimiter.
var Split = &Builtin{
	Name: "split",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.NewArray(nil, types.S),
	),
}

// Replace returns the given string with all instances of the second argument replaced
// by the third.
var Replace = &Builtin{
	Name: "replace",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
			types.S,
		),
		types.S,
	),
}

// Trim returns the given string will all leading or trailing instances of the second
// argument removed.
var Trim = &Builtin{
	Name: "trim",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.S,
	),
}

// Sprintf returns the given string, formatted.
var Sprintf = &Builtin{
	Name: "sprintf",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.NewArray(nil, types.A),
		),
		types.S,
	),
}

/**
 * JSON
 */

// JSONMarshal serializes the input term.
var JSONMarshal = &Builtin{
	Name: "json.marshal",
	Decl: types.NewFunction(
		types.Args(types.A),
		types.S,
	),
}

// JSONUnmarshal deserializes the input string.
var JSONUnmarshal = &Builtin{
	Name: "json.unmarshal",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.A,
	),
}

// Base64UrlEncode serializes the input string into base64url encoding.
var Base64UrlEncode = &Builtin{
	Name: "base64url.encode",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.S,
	),
}

// Base64UrlDecode deserializes the base64url encoded input string.
var Base64UrlDecode = &Builtin{
	Name: "base64url.decode",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.S,
	),
}

// YAMLMarshal serializes the input term.
var YAMLMarshal = &Builtin{
	Name: "yaml.marshal",
	Decl: types.NewFunction(
		types.Args(types.A),
		types.S,
	),
}

// YAMLUnmarshal deserializes the input string.
var YAMLUnmarshal = &Builtin{
	Name: "yaml.unmarshal",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.A,
	),
}

/**
 * Tokens
 */

// JWTDecode decodes a JSON Web Token and outputs it as an Object.
var JWTDecode = &Builtin{
	Name: "io.jwt.decode",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.NewArray([]types.Type{
			types.NewObject(nil, types.NewDynamicProperty(types.A, types.A)),
			types.NewObject(nil, types.NewDynamicProperty(types.A, types.A)),
			types.S,
		}, nil),
	),
}

/**
 * Time
 */

// NowNanos returns the current time since epoch in nanoseconds.
var NowNanos = &Builtin{
	Name: "time.now_ns",
	Decl: types.NewFunction(
		nil,
		types.N,
	),
}

// ParseNanos returns the time in nanoseconds parsed from the string in the given format.
var ParseNanos = &Builtin{
	Name: "time.parse_ns",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.S,
		),
		types.N,
	),
}

// ParseRFC3339Nanos returns the time in nanoseconds parsed from the string in RFC3339 format.
var ParseRFC3339Nanos = &Builtin{
	Name: "time.parse_rfc3339_ns",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.N,
	),
}

// ParseDurationNanos returns the duration in nanoseconds represented by a duration string.
// Duration string is similar to the Go time.ParseDuration string
var ParseDurationNanos = &Builtin{
	Name: "time.parse_duration_ns",
	Decl: types.NewFunction(
		types.Args(types.S),
		types.N,
	),
}

/**
 * Graphs.
 */

// WalkBuiltin generates [path, value] tuples for all nested documents
// (recursively).
var WalkBuiltin = &Builtin{
	Name: "walk",
	Decl: types.NewFunction(
		types.Args(types.A),
		types.NewArray(
			[]types.Type{
				types.NewArray(nil, types.A),
				types.A,
			},
			nil,
		),
	),
}

/**
 * Deprecated built-ins.
 */

// SetDiff has been replaced by the minus built-in.
var SetDiff = &Builtin{
	Name: "set_diff",
	Decl: types.NewFunction(
		types.Args(
			types.NewSet(types.A),
			types.NewSet(types.A),
		),
		types.NewSet(types.A),
	),
}

// Builtin represents a built-in function supported by OPA. Every built-in
// function is uniquely identified by a name.
type Builtin struct {
	Name  string          // Unique name of built-in function, e.g., <name>(arg1,arg2,...,argN)
	Infix string          // Unique name of infix operator. Default should be unset.
	Decl  *types.Function // Built-in function type declaration.
}

// Expr creates a new expression for the built-in with the given terms.
func (b *Builtin) Expr(terms ...*Term) *Expr {
	ts := make([]*Term, len(terms)+1)
	ts[0] = NewTerm(b.Ref())
	for i := range terms {
		ts[i+1] = terms[i]
	}
	return &Expr{
		Terms: ts,
	}
}

// Ref returns a Ref that refers to the built-in function.
func (b *Builtin) Ref() Ref {
	parts := strings.Split(b.Name, ".")
	ref := make(Ref, len(parts))
	ref[0] = VarTerm(parts[0])
	for i := 1; i < len(parts); i++ {
		ref[i] = StringTerm(parts[i])
	}
	return ref
}

// IsTargetPos returns true if a variable in the i-th position will be bound by
// evaluating the call expression.
func (b *Builtin) IsTargetPos(i int) bool {
	return len(b.Decl.Args()) == i
}

func init() {
	BuiltinMap = map[string]*Builtin{}
	for _, b := range DefaultBuiltins {
		RegisterBuiltin(b)
	}
}
