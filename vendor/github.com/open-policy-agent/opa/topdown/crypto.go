// Copyright 2018 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"crypto/x509"
	"encoding/json"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/util"
)

func builtinCryptoX509ParseCertificates(a ast.Value) (ast.Value, error) {

	str, err := builtinBase64Decode(a)
	if err != nil {
		return nil, err
	}

	certs, err := x509.ParseCertificates([]byte(str.(ast.String)))
	if err != nil {
		return nil, err
	}

	bs, err := json.Marshal(certs)
	if err != nil {
		return nil, err
	}

	var x interface{}

	if err := util.UnmarshalJSON(bs, &x); err != nil {
		return nil, err
	}

	return ast.InterfaceToValue(x)
}

func init() {
	RegisterFunctionalBuiltin1(ast.CryptoX509ParseCertificates.Name, builtinCryptoX509ParseCertificates)
}
