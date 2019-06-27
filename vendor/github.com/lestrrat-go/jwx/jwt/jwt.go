//go:generate go run internal/cmd/gentoken/main.go

// Package jwt implements JSON Web Tokens as described in https://tools.ietf.org/html/rfc7519
package jwt

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"strings"

	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jws"
	"github.com/pkg/errors"
)

// ParseString calls Parse with the given string
func ParseString(s string, options ...Option) (*Token, error) {
	return Parse(strings.NewReader(s), options...)
}

// ParseString calls Parse with the given byte sequence
func ParseBytes(s []byte, options ...Option) (*Token, error) {
	return Parse(bytes.NewReader(s), options...)
}

// Parse parses the JWT token payload and creates a new `jwt.Token` object.
// The token must be encoded in either JSON format or compact format.
//
// If the token is signed and you want to verify the payload, you must
// pass the jwt.WithVerify(alg, key) option. If you do not specify these
// parameters, no verification will be performed.
func Parse(src io.Reader, options ...Option) (*Token, error) {
	var params VerifyParameters
	for _, o := range options {
		switch o.Name() {
		case optkeyVerify:
			params = o.Value().(VerifyParameters)
		}
	}

	if params != nil {
		return ParseVerify(src, params.Algorithm(), params.Key())
	}

	m, err := jws.Parse(src)
	if err != nil {
		return nil, errors.Wrap(err, `invalid jws message`)
	}

	token := New()
	if err := json.Unmarshal(m.Payload(), token); err != nil {
		return nil, errors.Wrap(err, `failed to parse token`)
	}
	return token, nil
}

// ParseVerify is a function that is similar to Parse(), but does not
// allow for parsing without signature verification parameters.
func ParseVerify(src io.Reader, alg jwa.SignatureAlgorithm, key interface{}) (*Token, error) {
	data, err := ioutil.ReadAll(src)
	if err != nil {
		return nil, errors.Wrap(err, `failed to read token from source`)
	}

	v, err := jws.Verify(data, alg, key)
	if err != nil {
		return nil, errors.Wrap(err, `failed to verify jws signature`)
	}

	var token Token
	if err := json.Unmarshal(v, &token); err != nil {
		return nil, errors.Wrap(err, `failed to parse token`)
	}
	return &token, nil
}

// New creates a new empty JWT token
func New() *Token {
	return &Token{}
}

// Sign is a convenience function to create a signed JWT token serialized in
// compact form. `key` must match the key type required by the given
// signature method `method`
func (t *Token) Sign(method jwa.SignatureAlgorithm, key interface{}) ([]byte, error) {
	buf, err := json.Marshal(t)
	if err != nil {
		return nil, errors.Wrap(err, `failed to marshal token`)
	}

	var hdr jws.StandardHeaders
	if hdr.Set(`alg`, method.String()) != nil {
		return nil, errors.Wrap(err, `failed to sign payload`)
	}
	if hdr.Set(`typ`, `JWT`) != nil {
		return nil, errors.Wrap(err, `failed to sign payload`)
	}
	sign, err := jws.Sign(buf, method, key, jws.WithHeaders(&hdr))
	if err != nil {
		return nil, errors.Wrap(err, `failed to sign payload`)
	}

	return sign, nil
}
