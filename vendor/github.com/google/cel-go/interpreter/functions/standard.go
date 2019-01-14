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

package functions

import (
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// StandardOverloads returns the definitions of the built-in overloads.
func StandardOverloads() []*Overload {
	return []*Overload{
		// Logical not (!a)
		{
			Operator:     operators.LogicalNot,
			OperandTrait: traits.NegatorType,
			Unary: func(value ref.Value) ref.Value {
				return value.(traits.Negater).Negate()
			}},
		// Logical and (a && b)
		{
			Operator: operators.LogicalAnd,
			Binary:   logicalAnd},
		// Logical or (a || b)
		{
			Operator: operators.LogicalOr,
			Binary:   logicalOr},
		// Conditional operator (a ? b : c)
		{
			Operator: operators.Conditional,
			Function: conditional},
		// Not strictly false: IsBool(a) ? a : true
		{
			Operator: operators.NotStrictlyFalse,
			Unary:    notStrictlyFalse},
		// Deprecated: not strictly false, may be overridden in the environment.
		{
			Operator: operators.OldNotStrictlyFalse,
			Unary:    notStrictlyFalse},

		// Equality overloads
		{Operator: operators.Equals,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.Equal(rhs)
			}},

		{Operator: operators.NotEquals,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				eq := lhs.Equal(rhs)
				eqBool, isbool := eq.(types.Bool)
				if isbool {
					return !eqBool
				}
				return eq
			}},

		// Less than operator
		{Operator: operators.Less,
			OperandTrait: traits.ComparerType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				cmp := lhs.(traits.Comparer).Compare(rhs)
				if cmp == types.IntNegOne {
					return types.True
				}
				if cmp == types.IntOne || cmp == types.IntZero {
					return types.False
				}
				return cmp
			}},

		// Less than or equal operator
		{Operator: operators.LessEquals,
			OperandTrait: traits.ComparerType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				cmp := lhs.(traits.Comparer).Compare(rhs)
				if cmp == types.IntNegOne || cmp == types.IntZero {
					return types.True
				}
				if cmp == types.IntOne {
					return types.False
				}
				return cmp
			}},

		// Greater than operator
		{Operator: operators.Greater,
			OperandTrait: traits.ComparerType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				cmp := lhs.(traits.Comparer).Compare(rhs)
				if cmp == types.IntOne {
					return types.True
				}
				if cmp == types.IntNegOne || cmp == types.IntZero {
					return types.False
				}
				return cmp
			}},

		// Greater than equal operators
		{Operator: operators.GreaterEquals,
			OperandTrait: traits.ComparerType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				cmp := lhs.(traits.Comparer).Compare(rhs)
				if cmp == types.IntOne || cmp == types.IntZero {
					return types.True
				}
				if cmp == types.IntNegOne {
					return types.False
				}
				return cmp
			}},

		// TODO: Verify overflow, NaN, underflow cases for numeric values.

		// Add operator
		{Operator: operators.Add,
			OperandTrait: traits.AdderType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Adder).Add(rhs)
			}},

		// Subtract operators
		{Operator: operators.Subtract,
			OperandTrait: traits.SubtractorType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Subtractor).Subtract(rhs)
			}},

		// Multiply operator
		{Operator: operators.Multiply,
			OperandTrait: traits.MultiplierType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Multiplier).Multiply(rhs)
			}},

		// Divide operator
		{Operator: operators.Divide,
			OperandTrait: traits.DividerType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Divider).Divide(rhs)
			}},

		// Modulo operator
		{Operator: operators.Modulo,
			OperandTrait: traits.ModderType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Modder).Modulo(rhs)
			}},

		// Negate operator
		{Operator: operators.Negate,
			OperandTrait: traits.NegatorType,
			Unary: func(value ref.Value) ref.Value {
				return value.(traits.Negater).Negate()
			}},

		// Index operator
		{Operator: operators.Index,
			OperandTrait: traits.IndexerType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Indexer).Get(rhs)
			}},

		// Size function
		{Operator: overloads.Size,
			OperandTrait: traits.SizerType,
			Unary: func(value ref.Value) ref.Value {
				return value.(traits.Sizer).Size()
			}},

		// In operator
		{Operator: operators.In, Binary: inAggregate},
		// Deprecated: in operator, may be overridden in the environment.
		{Operator: operators.OldIn, Binary: inAggregate},

		// Matches function
		{Operator: overloads.Matches,
			OperandTrait: traits.MatcherType,
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				return lhs.(traits.Matcher).Match(rhs)
			}},

		// Type conversion functions
		// TODO: verify type conversion safety of numeric values.

		// Int conversions.
		{Operator: overloads.TypeConvertInt,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.IntType)
			}},

		// Uint conversions.
		{Operator: overloads.TypeConvertUint,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.UintType)
			}},

		// Double conversions.
		{Operator: overloads.TypeConvertDouble,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.DoubleType)
			}},

		// Bool conversions.
		{Operator: overloads.TypeConvertBool,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.BoolType)
			}},

		// Bytes conversions.
		{Operator: overloads.TypeConvertBytes,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.BytesType)
			}},

		// String conversions.
		{Operator: overloads.TypeConvertString,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.StringType)
			}},

		// Timestamp conversions.
		{Operator: overloads.TypeConvertTimestamp,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.TimestampType)
			}},

		// Duration conversions.
		{Operator: overloads.TypeConvertDuration,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.DurationType)
			}},

		// Type operations.
		{Operator: overloads.TypeConvertType,
			Unary: func(value ref.Value) ref.Value {
				return value.ConvertToType(types.TypeType)
			}},

		{Operator: overloads.Iterator,
			OperandTrait: traits.IterableType,
			Unary: func(value ref.Value) ref.Value {
				return value.(traits.Iterable).Iterator()
			}},

		{Operator: overloads.HasNext,
			OperandTrait: traits.IteratorType,
			Unary: func(value ref.Value) ref.Value {
				return value.(traits.Iterator).HasNext()
			}},

		{Operator: overloads.Next,
			OperandTrait: traits.IteratorType,
			Unary: func(value ref.Value) ref.Value {
				return value.(traits.Iterator).Next()
			}},
	}

}

func logicalAnd(lhs ref.Value, rhs ref.Value) ref.Value {
	lhsIsBool := types.Bool(types.IsBool(lhs))
	rhsIsBool := types.Bool(types.IsBool(rhs))
	// both are boolean use natural logic.
	if lhsIsBool && rhsIsBool {
		return lhs.(types.Bool) && rhs.(types.Bool)
	}
	// one or the other is boolean and false, return false.
	if lhsIsBool && !lhs.(types.Bool) ||
		rhsIsBool && !rhs.(types.Bool) {
		return types.False
	}

	if types.IsUnknown(lhs) {
		return lhs
	}

	if types.IsUnknown(rhs) {
		return rhs
	}

	// if the left-hand side is non-boolean return it as the error.
	if !lhsIsBool {
		return types.NewErr("Got '%v', expected argument of type 'bool'", lhs)
	}
	return types.NewErr("Got '%v', expected argument of type 'bool'", rhs)
}

func logicalOr(lhs ref.Value, rhs ref.Value) ref.Value {
	lhsIsBool := types.Bool(types.IsBool(lhs))
	rhsIsBool := types.Bool(types.IsBool(rhs))
	// both are boolean, use natural logic.
	if lhsIsBool && rhsIsBool {
		return lhs.(types.Bool) || rhs.(types.Bool)
	}
	// one or the other is boolean and true, return true
	if lhsIsBool && lhs.(types.Bool) ||
		rhsIsBool && rhs.(types.Bool) {
		return types.True
	}

	if types.IsUnknown(lhs) {
		return lhs
	}
	if types.IsUnknown(rhs) {
		return rhs
	}

	// if the left-hand side is non-boolean return it as the error.
	if !lhsIsBool {
		return types.NewErr("Got '%v', expected argument of type 'bool'", lhs)
	}
	return types.NewErr("Got '%v', expected argument of type 'bool'", rhs)
}

func conditional(values ...ref.Value) ref.Value {
	if len(values) != 3 {
		return types.NewErr("no such overload")
	}
	cond := values[0]
	condType := cond.Type()
	if types.IsBool(condType) {
		if cond == types.True {
			return values[1]
		}
		return values[2]
	} else if types.IsError(condType) || types.IsUnknown(condType) {
		return cond
	} else {
		return types.NewErr("no such overload")
	}
}

func notStrictlyFalse(value ref.Value) ref.Value {
	if types.IsBool(value) {
		return value
	}
	return types.True
}

func inAggregate(lhs ref.Value, rhs ref.Value) ref.Value {
	if rhs.Type().HasTrait(traits.ContainerType) {
		return rhs.(traits.Container).Contains(lhs)
	}
	return types.NewErr("no such overload")
}
