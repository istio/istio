package datapoint

import (
	"fmt"
	"strconv"
)

// A Value is the value being sent between servers.  It is usually a floating point
// but different systems support different types of values.  All values must be printable in some
// human readable interface
type Value interface {
	fmt.Stringer
}

type floatWire float64

// FloatValue are values that represent an exact floating point value.
type FloatValue interface {
	Value
	Float() float64
}

func (wireVal floatWire) Float() float64 {
	return float64(wireVal)
}

func (wireVal floatWire) String() string {
	return strconv.FormatFloat(float64(wireVal), 'f', -1, 64)
}

// NewFloatValue creates new datapoint value is a float
func NewFloatValue(val float64) FloatValue {
	return floatWire(val)
}

type intWire int64

// IntValue are values that represent an exact integer point value.
type IntValue interface {
	Value
	Int() int64
}

func (wireVal intWire) Int() int64 {
	return int64(wireVal)
}

func (wireVal intWire) String() string {
	return strconv.FormatInt(int64(wireVal), 10)
}

// NewIntValue creates new datapoint value is an integer
func NewIntValue(val int64) IntValue {
	return intWire(val)
}

type strWire string

// StringValue are values that represent an exact string value and don't have a numeric
// representation.
type StringValue interface {
	Value
}

func (wireVal strWire) String() string {
	return string(wireVal)
}

// NewStringValue creates new datapoint value is a string
func NewStringValue(val string) StringValue {
	return strWire(val)
}
