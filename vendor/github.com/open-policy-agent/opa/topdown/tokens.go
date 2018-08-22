// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

var (
	jwtEncKey = ast.StringTerm("enc")
	jwtCtyKey = ast.StringTerm("cty")
)

// Implements JWT decoding/validation based on RFC 7519 Section 7.2:
// https://tools.ietf.org/html/rfc7519#section-7.2
// It does no data validation, it merely checks that the given string
// represents a structurally valid JWT. It supports JWTs using JWS compact
// serialization.
func builtinJWTDecode(a ast.Value) (ast.Value, error) {
	astEncode, err := builtins.StringOperand(a, 1)
	encoding := string(astEncode)
	if !strings.Contains(encoding, ".") {
		return nil, errors.New("encoded JWT had no period separators")
	}

	parts := strings.Split(encoding, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("encoded JWT must have 3 sections, found %d", len(parts))
	}

	h, err := builtinBase64UrlDecode(ast.String(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("JWT header had invalid encoding: %v", err)
	}

	header, err := validateJWTHeader(string(h.(ast.String)))
	if err != nil {
		return nil, err
	}

	p, err := builtinBase64UrlDecode(ast.String(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("JWT payload had invalid encoding: %v", err)
	}

	if cty := header.Get(jwtCtyKey); cty != nil {
		ctyVal := string(cty.Value.(ast.String))
		// It is possible for the contents of a token to be another
		// token as a result of nested signing or encryption. To handle
		// the case where we are given a token such as this, we check
		// the content type and recurse on the payload if the content
		// is "JWT".
		// When the payload is itself another encoded JWT, then its
		// contents are quoted (behavior of https://jwt.io/). To fix
		// this, remove leading and trailing quotes.
		if ctyVal == "JWT" {
			p, err = builtinTrim(p, ast.String(`"'`))
			if err != nil {
				panic("not reached")
			}
			return builtinJWTDecode(p)
		}
	}

	payload, err := extractJSONObject(string(p.(ast.String)))
	if err != nil {
		return nil, err
	}

	s, err := builtinBase64UrlDecode(ast.String(parts[2]))
	if err != nil {
		return nil, fmt.Errorf("JWT signature had invalid encoding: %v", err)
	}
	sign := hex.EncodeToString([]byte(s.(ast.String)))

	arr := make(ast.Array, 3)
	arr[0] = ast.NewTerm(header)
	arr[1] = ast.NewTerm(payload)
	arr[2] = ast.StringTerm(sign)

	return arr, nil
}

// Extract, validate and return the JWT header as an ast.Object.
func validateJWTHeader(h string) (ast.Object, error) {
	header, err := extractJSONObject(h)
	if err != nil {
		return nil, fmt.Errorf("bad JWT header: %v", err)
	}

	// There are two kinds of JWT tokens, a JSON Web Signature (JWS) and
	// a JSON Web Encryption (JWE). The latter is very involved, and we
	// won't support it for now.
	// This code checks which kind of JWT we are dealing with according to
	// RFC 7516 Section 9: https://tools.ietf.org/html/rfc7516#section-9
	if header.Get(jwtEncKey) != nil {
		return nil, errors.New("JWT is a JWE object, which is not supported")
	}

	return header, nil
}

func extractJSONObject(s string) (ast.Object, error) {
	// XXX: This code relies on undocumented behavior of Go's
	// json.Unmarshal using the last occurrence of duplicate keys in a JSON
	// Object. If duplicate keys are present in a JWT, the last must be
	// used or the token rejected. Since detecting duplicates is tantamount
	// to parsing it ourselves, we're relying on the Go implementation
	// using the last occuring instance of the key, which is the behavior
	// as of Go 1.8.1.
	v, err := builtinJSONUnmarshal(ast.String(s))
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}

	o, ok := v.(ast.Object)
	if !ok {
		return nil, errors.New("decoded JSON type was not an Object")
	}

	return o, nil
}

func init() {
	RegisterFunctionalBuiltin1(ast.JWTDecode.Name, builtinJWTDecode)
}
