// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

func builtinFormatInt(a, b ast.Value) (ast.Value, error) {

	input, err := builtins.NumberOperand(a, 1)
	if err != nil {
		return nil, err
	}

	base, err := builtins.NumberOperand(b, 2)
	if err != nil {
		return nil, err
	}

	var format string
	switch base {
	case ast.Number("2"):
		format = "%b"
	case ast.Number("8"):
		format = "%o"
	case ast.Number("10"):
		format = "%d"
	case ast.Number("16"):
		format = "%x"
	default:
		return nil, builtins.NewOperandEnumErr(2, "2", "8", "10", "16")
	}

	f := builtins.NumberToFloat(input)
	i, _ := f.Int(nil)

	return ast.String(fmt.Sprintf(format, i)), nil
}

func builtinConcat(a, b ast.Value) (ast.Value, error) {

	join, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	strs := []string{}

	switch b := b.(type) {
	case ast.Array:
		for i := range b {
			s, ok := b[i].Value.(ast.String)
			if !ok {
				return nil, builtins.NewOperandElementErr(2, b, b[i].Value, "string")
			}
			strs = append(strs, string(s))
		}
	case ast.Set:
		err := b.Iter(func(x *ast.Term) error {
			s, ok := x.Value.(ast.String)
			if !ok {
				return builtins.NewOperandElementErr(2, b, x.Value, "string")
			}
			strs = append(strs, string(s))
			return nil
		})
		if err != nil {
			return nil, err
		}
	default:
		return nil, builtins.NewOperandTypeErr(2, b, "set", "array")
	}

	return ast.String(strings.Join(strs, string(join))), nil
}

func builtinIndexOf(a, b ast.Value) (ast.Value, error) {
	base, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	search, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	index := strings.Index(string(base), string(search))
	return ast.IntNumberTerm(index).Value, nil
}

func builtinSubstring(a, b, c ast.Value) (ast.Value, error) {

	base, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	startIndex, err := builtins.IntOperand(b, 2)
	if err != nil {
		return nil, err
	}

	length, err := builtins.IntOperand(c, 3)
	if err != nil {
		return nil, err
	}

	var s ast.String
	if length < 0 {
		s = ast.String(base[startIndex:])
	} else {
		upto := startIndex + length
		if len(base) < upto {
			upto = len(base)
		}
		s = ast.String(base[startIndex:upto])
	}

	return s, nil
}

func builtinContains(a, b ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	substr, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	return ast.Boolean(strings.Contains(string(s), string(substr))), nil
}

func builtinStartsWith(a, b ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	prefix, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	return ast.Boolean(strings.HasPrefix(string(s), string(prefix))), nil
}

func builtinEndsWith(a, b ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	suffix, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	return ast.Boolean(strings.HasSuffix(string(s), string(suffix))), nil
}

func builtinLower(a ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	return ast.String(strings.ToLower(string(s))), nil
}

func builtinUpper(a ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	return ast.String(strings.ToUpper(string(s))), nil
}

func builtinSplit(a, b ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}
	d, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}
	elems := strings.Split(string(s), string(d))
	arr := make(ast.Array, len(elems))
	for i := range arr {
		arr[i] = ast.StringTerm(elems[i])
	}
	return arr, nil
}

func builtinReplace(a, b, c ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	old, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	new, err := builtins.StringOperand(c, 3)
	if err != nil {
		return nil, err
	}

	return ast.String(strings.Replace(string(s), string(old), string(new), -1)), nil
}

func builtinTrim(a, b ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	c, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}

	return ast.String(strings.Trim(string(s), string(c))), nil
}

func builtinSprintf(a, b ast.Value) (ast.Value, error) {
	s, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	astArr, ok := b.(ast.Array)
	if !ok {
		return nil, builtins.NewOperandTypeErr(2, b, "array")
	}

	args := make([]interface{}, len(astArr))

	for i := range astArr {
		switch v := astArr[i].Value.(type) {
		case ast.Number:
			if n, ok := v.Int(); ok {
				args[i] = n
			} else if f, ok := v.Float64(); ok {
				args[i] = f
			} else {
				args[i] = v.String()
			}
		case ast.String:
			args[i] = string(v)
		default:
			args[i] = astArr[i].String()
		}
	}

	return ast.String(fmt.Sprintf(string(s), args...)), nil
}

func init() {
	RegisterFunctionalBuiltin2(ast.FormatInt.Name, builtinFormatInt)
	RegisterFunctionalBuiltin2(ast.Concat.Name, builtinConcat)
	RegisterFunctionalBuiltin2(ast.IndexOf.Name, builtinIndexOf)
	RegisterFunctionalBuiltin3(ast.Substring.Name, builtinSubstring)
	RegisterFunctionalBuiltin2(ast.Contains.Name, builtinContains)
	RegisterFunctionalBuiltin2(ast.StartsWith.Name, builtinStartsWith)
	RegisterFunctionalBuiltin2(ast.EndsWith.Name, builtinEndsWith)
	RegisterFunctionalBuiltin1(ast.Upper.Name, builtinUpper)
	RegisterFunctionalBuiltin1(ast.Lower.Name, builtinLower)
	RegisterFunctionalBuiltin2(ast.Split.Name, builtinSplit)
	RegisterFunctionalBuiltin3(ast.Replace.Name, builtinReplace)
	RegisterFunctionalBuiltin2(ast.Trim.Name, builtinTrim)
	RegisterFunctionalBuiltin2(ast.Sprintf.Name, builtinSprintf)
}
