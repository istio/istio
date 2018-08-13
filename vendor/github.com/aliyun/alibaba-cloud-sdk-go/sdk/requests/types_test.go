package requests

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewInteger(t *testing.T) {
	integer := NewInteger(123123)
	assert.True(t, integer.hasValue())
	value, err := integer.getValue()
	assert.Nil(t, err)
	assert.Equal(t, 123123, value)
	var expected Integer
	expected = "123123"
	assert.Equal(t, expected, integer)
}

func TestNewInteger64(t *testing.T) {
	long := NewInteger64(123123123123123123)
	assert.True(t, long.hasValue())
	value, err := long.getValue64()
	assert.Nil(t, err)
	assert.Equal(t, int64(123123123123123123), value)
	var expected Integer
	expected = "123123123123123123"
	assert.Equal(t, expected, long)
}

func TestNewBoolean(t *testing.T) {
	boolean := NewBoolean(false)
	assert.True(t, boolean.hasValue())
	value, err := boolean.getValue()
	assert.Nil(t, err)
	assert.Equal(t, false, value)
	var expected Boolean
	expected = "false"
	assert.Equal(t, expected, boolean)
}

func TestNewFloat(t *testing.T) {
	float := NewFloat(123123.123123)
	assert.True(t, float.hasValue())
	value, err := float.getValue()
	assert.Nil(t, err)
	assert.Equal(t, 123123.123123, value)
	var expected Float
	expected = "123123.123123"
	assert.Equal(t, expected, float)
}
