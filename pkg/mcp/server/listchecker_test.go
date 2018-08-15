//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/credentials"
)

func TestListAuthChecker(t *testing.T) {
	testCases := []struct {
		name         string
		authInfo     credentials.AuthInfo
		extractIDsFn func(exts []pkix.Extension) ([]string, error)
		err          string
		remove       bool // Remove the added entry
		set          bool // Use set to add the entry
	}{
		{
			name:     "nil",
			authInfo: nil,
			err:      "denying by default",
		},
		{
			name:     "non-tlsinfo",
			authInfo: &nonTLSInfo{},
			err:      "unable to extract TLS info",
		},
		{
			name:     "empty tlsinfo",
			authInfo: credentials.TLSInfo{},
			err:      "no allowed identity found in peer's authentication info",
		},
		{
			name: "empty cert chain",
			authInfo: credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}},
			},

			err: "no allowed identity found in peer's authentication info",
		},
		{
			name: "error extracting ids",
			authInfo: credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}},
			},
			extractIDsFn: func(exts []pkix.Extension) ([]string, error) {
				return nil, fmt.Errorf("error extracting ids")
			},
			err: "no allowed identity found in peer's authentication info",
		},
		{
			name: "id mismatch",
			authInfo: credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}},
			},
			extractIDsFn: func(exts []pkix.Extension) ([]string, error) {
				return []string{"bar"}, nil
			},
			err: "no allowed identity found in peer's authentication info",
		},
		{
			name: "success",
			authInfo: credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}},
			},
			extractIDsFn: func(exts []pkix.Extension) ([]string, error) {
				return []string{"foo"}, nil
			},
		},
		{
			name: "removed",
			authInfo: credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}},
			},
			extractIDsFn: func(exts []pkix.Extension) ([]string, error) {
				return []string{"foo"}, nil
			},
			remove: true,
			err:    "no allowed identity found in peer's authentication info",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {

			c := NewListAuthChecker()
			if testCase.extractIDsFn != nil {
				c.extractIDsFn = testCase.extractIDsFn
			}

			if testCase.set {
				c.Set("foo")
			} else {
				c.Add("foo")
			}

			if testCase.remove {
				c.Remove("foo")
			}

			err := c.Check(testCase.authInfo)
			if err != nil {
				if testCase.err == "" {
					t.Fatalf("Unexpected error: %v", err)
				} else if !strings.HasPrefix(err.Error(), testCase.err) {
					t.Fatalf("Error mismatch: got:%v, expected:%s", err, testCase.err)
				}
			} else if testCase.err != "" {
				t.Fatalf("Expected error not found: %s", testCase.err)
			}
		})
	}
}

type nonTLSInfo struct {
}

var _ credentials.AuthInfo = &nonTLSInfo{}

func (a *nonTLSInfo) AuthType() string {
	return "non-tls"
}

type authInfo struct {
	credentials.TLSInfo
}

var _ credentials.AuthInfo = &authInfo{}

func (a *authInfo) AuthType() string {
	return ""
}
