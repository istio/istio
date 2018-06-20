// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"reflect"
	"testing"
)

func TestParseJwksURI(t *testing.T) {
	cases := []struct {
		in                   string
		expectedHostname     string
		expectedPort         *Port
		expectedUseSSL       bool
		expectedErrorMessage string
	}{
		{
			in:                   "foo.bar.com",
			expectedErrorMessage: `URI scheme "" is not supported`,
		},
		{
			in:                   "tcp://foo.bar.com:abc",
			expectedErrorMessage: `URI scheme "tcp" is not supported`,
		},
		{
			in:                   "http://foo.bar.com:abc",
			expectedErrorMessage: `strconv.Atoi: parsing "abc": invalid syntax`,
		},
		{
			in:               "http://foo.bar.com",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "http", Port: 80},
			expectedUseSSL:   false,
		},
		{
			in:               "https://foo.bar.com",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "https", Port: 443},
			expectedUseSSL:   true,
		},
		{
			in:               "http://foo.bar.com:1234",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "http", Port: 1234},
			expectedUseSSL:   false,
		},
		{
			in:               "https://foo.bar.com:1234/secure/key",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "https", Port: 1234},
			expectedUseSSL:   true,
		},
	}
	for _, c := range cases {
		host, port, useSSL, err := ParseJwksURI(c.in)
		if err != nil {
			if c.expectedErrorMessage != err.Error() {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got (%v)", c.in, c.expectedErrorMessage, err)
			}
		} else {
			if c.expectedErrorMessage != "" {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got no error", c.in, c.expectedErrorMessage)
			}
			if c.expectedHostname != host || !reflect.DeepEqual(c.expectedPort, port) || c.expectedUseSSL != useSSL {
				t.Errorf("ParseJwksURI(%s): expected (%s, %#v, %v), got (%s, %#v, %v)",
					c.in, c.expectedHostname, c.expectedPort, c.expectedUseSSL,
					host, port, useSSL)
			}
		}
	}
}
