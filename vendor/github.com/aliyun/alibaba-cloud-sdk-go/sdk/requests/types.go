package requests

import "strconv"

type Integer string

func NewInteger(integer int) Integer {
	return Integer(strconv.Itoa(integer))
}

func (integer Integer) hasValue() bool {
	return integer != ""
}

func (integer Integer) getValue() (int, error) {
	return strconv.Atoi(string(integer))
}

func NewInteger64(integer int64) Integer {
	return Integer(strconv.FormatInt(integer, 10))
}

func (integer Integer) getValue64() (int64, error) {
	return strconv.ParseInt(string(integer), 10, 0)
}

type Boolean string

func NewBoolean(bool bool) Boolean {
	return Boolean(strconv.FormatBool(bool))
}

func (boolean Boolean) hasValue() bool {
	return boolean != ""
}

func (boolean Boolean) getValue() (bool, error) {
	return strconv.ParseBool(string(boolean))
}

type Float string

func NewFloat(f float64) Float {
	return Float(strconv.FormatFloat(f, 'f', 6, 64))
}

func (float Float) hasValue() bool {
	return float != ""
}

func (float Float) getValue() (float64, error) {
	return strconv.ParseFloat(string(float), 64)
}
