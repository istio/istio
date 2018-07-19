// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	ghodss "github.com/ghodss/yaml"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/util"
)

func builtinJSONMarshal(a ast.Value) (ast.Value, error) {

	asJSON, err := ast.JSON(a)
	if err != nil {
		return nil, err
	}

	bs, err := json.Marshal(asJSON)
	if err != nil {
		return nil, err
	}

	return ast.String(string(bs)), nil
}

func builtinJSONUnmarshal(a ast.Value) (ast.Value, error) {

	str, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	var x interface{}

	if err := util.UnmarshalJSON([]byte(str), &x); err != nil {
		return nil, err
	}

	return ast.InterfaceToValue(x)
}

func builtinBase64UrlEncode(a ast.Value) (ast.Value, error) {
	str, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	return ast.String(base64.URLEncoding.EncodeToString([]byte(str))), nil
}

func builtinBase64UrlDecode(a ast.Value) (ast.Value, error) {
	str, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}
	s := string(str)

	// Some base64url encoders omit the padding at the end, so this case
	// corrects such representations using the method given in RFC 7515
	// Appendix C: https://tools.ietf.org/html/rfc7515#appendix-C
	if !strings.HasSuffix(s, "=") {
		switch len(s) % 4 {
		case 0:
		case 2:
			s += "=="
		case 3:
			s += "="
		default:
			return nil, fmt.Errorf("illegal base64url string: %s", s)
		}
	}
	result, err := base64.URLEncoding.DecodeString(s)
	return ast.String(result), err
}

func builtinYAMLMarshal(a ast.Value) (ast.Value, error) {

	asJSON, err := ast.JSON(a)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	if err := encoder.Encode(asJSON); err != nil {
		return nil, err
	}

	bs, err := ghodss.JSONToYAML(buf.Bytes())
	if err != nil {
		return nil, err
	}

	return ast.String(string(bs)), nil
}

func builtinYAMLUnmarshal(a ast.Value) (ast.Value, error) {

	str, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	bs, err := ghodss.YAMLToJSON([]byte(str))
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(bs)
	decoder := util.NewJSONDecoder(buf)
	var val interface{}
	err = decoder.Decode(&val)
	if err != nil {
		return nil, err
	}

	return ast.InterfaceToValue(val)
}

func init() {
	RegisterFunctionalBuiltin1(ast.JSONMarshal.Name, builtinJSONMarshal)
	RegisterFunctionalBuiltin1(ast.JSONUnmarshal.Name, builtinJSONUnmarshal)
	RegisterFunctionalBuiltin1(ast.Base64UrlEncode.Name, builtinBase64UrlEncode)
	RegisterFunctionalBuiltin1(ast.Base64UrlDecode.Name, builtinBase64UrlDecode)
	RegisterFunctionalBuiltin1(ast.YAMLMarshal.Name, builtinYAMLMarshal)
	RegisterFunctionalBuiltin1(ast.YAMLUnmarshal.Name, builtinYAMLUnmarshal)
}
