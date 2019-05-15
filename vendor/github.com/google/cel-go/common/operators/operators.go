// Copyright 2018 Google LLC
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

// Package operators defines the internal function names of operators.
//
// ALl operators in the expression language are modelled as function calls.
package operators

// String "names" for CEL operators.
const (
	// Symbolic operators.
	Conditional   = "_?_:_"
	LogicalAnd    = "_&&_"
	LogicalOr     = "_||_"
	LogicalNot    = "!_"
	Equals        = "_==_"
	NotEquals     = "_!=_"
	Less          = "_<_"
	LessEquals    = "_<=_"
	Greater       = "_>_"
	GreaterEquals = "_>=_"
	Add           = "_+_"
	Subtract      = "_-_"
	Multiply      = "_*_"
	Divide        = "_/_"
	Modulo        = "_%_"
	Negate        = "-_"
	Index         = "_[_]"

	// Macros, must have a valid identifier.
	Has       = "has"
	All       = "all"
	Exists    = "exists"
	ExistsOne = "exists_one"
	Map       = "map"
	Filter    = "filter"

	// Named operators, must not have be valid identifiers.
	NotStrictlyFalse = "@not_strictly_false"
	In               = "@in"

	// Deprecated: named operators with valid identifiers.
	OldNotStrictlyFalse = "__not_strictly_false__"
	OldIn               = "_in_"
)

var operators = map[string]string{
	"+":  Add,
	"-":  Subtract,
	"*":  Multiply,
	"/":  Divide,
	"%":  Modulo,
	"in": In,
	"==": Equals,
	"!=": NotEquals,
	"<":  Less,
	"<=": LessEquals,
	">":  Greater,
	">=": GreaterEquals,
}

// Find the internal function name for an operator, if the input text is one.
func Find(text string) (string, bool) {
	op, found := operators[text]
	return op, found
}
