// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

var (
	jwtEncKey = ast.StringTerm("enc")
	jwtCtyKey = ast.StringTerm("cty")
)

// JSONWebToken represent the 3 parts (header, payload & signature) of
//              a JWT in Base64.
type JSONWebToken struct {
	header    string
	payload   string
	signature string
}

// Implements JWT decoding/validation based on RFC 7519 Section 7.2:
// https://tools.ietf.org/html/rfc7519#section-7.2
// It does no data validation, it merely checks that the given string
// represents a structurally valid JWT. It supports JWTs using JWS compact
// serialization.
func builtinJWTDecode(a ast.Value) (ast.Value, error) {
	token, err := decodeJWT(a)
	if err != nil {
		return nil, err
	}

	h, err := builtinBase64UrlDecode(ast.String(token.header))
	if err != nil {
		return nil, fmt.Errorf("JWT header had invalid encoding: %v", err)
	}

	header, err := validateJWTHeader(string(h.(ast.String)))
	if err != nil {
		return nil, err
	}

	p, err := builtinBase64UrlDecode(ast.String(token.payload))
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

	s, err := builtinBase64UrlDecode(ast.String(token.signature))
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

// Implements RS256 JWT signature verification
func builtinJWTVerifyRS256(a ast.Value, b ast.Value) (ast.Value, error) {
	// Decode the JSON Web Token
	token, err := decodeJWT(a)
	if err != nil {
		return nil, err
	}

	// Process PEM encoded certificate input
	astCertificate, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}
	certificate := string(astCertificate)

	block, rest := pem.Decode([]byte(certificate))

	if block == nil || block.Type != "CERTIFICATE" || len(rest) > 0 {
		return nil, fmt.Errorf("failed to decode PEM block containing certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "PEM parse error")
	}

	// Get public key
	publicKey := cert.PublicKey.(*rsa.PublicKey)

	signature, err := token.decodeSignature()
	if err != nil {
		return nil, err
	}

	// Validate the JWT signature
	err = rsa.VerifyPKCS1v15(
		publicKey,
		crypto.SHA256,
		getInputSHA([]byte(token.header+"."+token.payload)),
		[]byte(signature))

	if err != nil {
		return ast.Boolean(false), nil
	}
	return ast.Boolean(true), nil
}

// Implements HS256 (secret) JWT signature verification
func builtinJWTVerifyHS256(a ast.Value, b ast.Value) (ast.Value, error) {
	// Decode the JSON Web Token
	token, err := decodeJWT(a)
	if err != nil {
		return nil, err
	}

	// Process Secret input
	astSecret, err := builtins.StringOperand(b, 2)
	if err != nil {
		return nil, err
	}
	secret := string(astSecret)

	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte(token.header + "." + token.payload))
	if err != nil {
		return nil, err
	}

	signature, err := token.decodeSignature()
	if err != nil {
		return nil, err
	}

	return ast.Boolean(hmac.Equal([]byte(signature), mac.Sum(nil))), nil
}

func decodeJWT(a ast.Value) (*JSONWebToken, error) {
	// Parse the JSON Web Token
	astEncode, err := builtins.StringOperand(a, 1)
	if err != nil {
		return nil, err
	}

	encoding := string(astEncode)
	if !strings.Contains(encoding, ".") {
		return nil, errors.New("encoded JWT had no period separators")
	}

	parts := strings.Split(encoding, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("encoded JWT must have 3 sections, found %d", len(parts))
	}

	return &JSONWebToken{header: parts[0], payload: parts[1], signature: parts[2]}, nil
}

func (token *JSONWebToken) decodeSignature() (string, error) {
	decodedSignature, err := builtinBase64UrlDecode(ast.String(token.signature))
	if err != nil {
		return "", err
	}

	signatureAst, err := builtins.StringOperand(decodedSignature, 1)
	if err != nil {
		return "", err
	}
	return string(signatureAst), err
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
	// using the last occurring instance of the key, which is the behavior
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

// getInputSha returns the SHA256 checksum of the input
func getInputSHA(input []byte) (hash []byte) {
	hasher := sha256.New()
	hasher.Write(input)
	return hasher.Sum(nil)
}

func init() {
	RegisterFunctionalBuiltin1(ast.JWTDecode.Name, builtinJWTDecode)
	RegisterFunctionalBuiltin2(ast.JWTVerifyRS256.Name, builtinJWTVerifyRS256)
	RegisterFunctionalBuiltin2(ast.JWTVerifyHS256.Name, builtinJWTVerifyHS256)
}
