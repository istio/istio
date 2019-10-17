// Copyright 2019 Istio Authors
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
	"strings"
	"testing"
)

func TestStringMatch(t *testing.T) {
	testCases := []struct {
		Name   string
		S      string
		List   []string
		Expect bool
	}{
		{
			Name: "exact match", S: "product page", List: []string{"review page", "product page"},
			Expect: true,
		},
		{
			Name: "wild character match", S: "product page", List: []string{"review page", "*"},
			Expect: true,
		},
		{
			Name: "prefix match", S: "product page", List: []string{"review page", "product*"},
			Expect: true,
		},
		{
			Name: "suffix match", S: "product page", List: []string{"review page", "*page"},
			Expect: true,
		},
		{
			Name: "not matched", S: "product page", List: []string{"review page", "xyz product page"},
			Expect: false,
		},
		{
			Name: "* in string", S: "*local", List: []string{"cluster.local"},
			Expect: true,
		},
		{
			Name: "* in list", S: "cluster.local", List: []string{"*local"},
			Expect: true,
		},
	}

	for _, tc := range testCases {
		if actual := stringMatch(tc.S, tc.List); actual != tc.Expect {
			t.Errorf("%s: expecting: %v, but got: %v", tc.Name, tc.Expect, actual)
		}
	}
}

func TestConvertToPort(t *testing.T) {
	testCases := []struct {
		Name   string
		V      string
		Expect uint32
		Err    string
	}{
		{
			Name: "negative port",
			V:    "-80",
			Err:  "invalid port -80:",
		},
		{
			Name: "invalid port",
			V:    "xyz",
			Err:  "invalid port xyz:",
		},
		{
			Name: "port too large",
			V:    "91234",
			Err:  "invalid port 91234:",
		},
		{
			Name:   "valid port",
			V:      "443",
			Expect: 443,
		},
	}

	for _, tc := range testCases {
		actual, err := convertToPort(tc.V)
		if tc.Err != "" {
			if err == nil {
				t.Errorf("%s: expecting error %s but found no error", tc.Name, tc.Err)
			} else if !strings.HasPrefix(err.Error(), tc.Err) {
				t.Errorf("%s: expecting error %s, but got: %s", tc.Name, tc.Err, err.Error())
			}
		} else if tc.Expect != actual {
			t.Errorf("%s: expecting %d, but got %d", tc.Name, tc.Expect, actual)
		}
	}
}

func TestConvertPortsToString(t *testing.T) {
	testCases := []struct {
		Name   string
		V      []int32
		Expect []string
		Err    string
	}{
		{
			Name:   "valid ports",
			V:      []int32{80, 3000, 443},
			Expect: []string{"80", "3000", "443"},
		},
		{
			Name:   "valid port",
			V:      []int32{9080},
			Expect: []string{"9080"},
		},
	}

	for _, tc := range testCases {
		actual := convertPortsToString(tc.V)
		for i := range tc.Expect {
			if tc.Expect[i] != actual[i] {
				t.Errorf("%s: expecting %s, but got %s", tc.Name, tc.Expect, actual)
			}
		}
	}
}

func TestIsKeyBinary(t *testing.T) {
	cases := []struct {
		s      string
		expect bool
	}{
		{s: "a[b]", expect: true},
		{s: "a", expect: false},
		{s: "a.b", expect: false},
		{s: "a.b[c]", expect: true},
		{s: "a.b[c.d]", expect: true},
		{s: "[a]", expect: false},
		{s: "[a", expect: false},
		{s: "a]", expect: false},
		{s: "a[]", expect: false},
		{s: "a.b[c.d]e", expect: false},
		{s: "a.b[[c.d]]", expect: true},
	}

	for _, c := range cases {
		if isKeyBinary(c.s) != c.expect {
			t.Errorf("isKeyBinary returned incorrect result for key: %s", c.s)
		}
	}
}

func TestExtractNameInBrackets(t *testing.T) {
	cases := []struct {
		s      string
		expect string
		err    bool
	}{
		{s: "[good]", expect: "good", err: false},
		{s: "[[good]]", expect: "[good]", err: false},
		{s: "[]", expect: "", err: false},
		{s: "[bad", expect: "", err: true},
		{s: "bad]", expect: "", err: true},
		{s: "bad", expect: "", err: true},
	}

	for _, c := range cases {
		s, err := extractNameInBrackets(c.s)
		if s != c.expect {
			t.Errorf("expecting [good] but found %s", s)
		}
		if c.err != (err != nil) {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestExtractActualServiceAccount(t *testing.T) {
	cases := []struct {
		in     string
		expect string
	}{
		{in: "service-account", expect: "service-account"},
		{in: "spiffe://xyz.com/sa/test-sa/ns/default", expect: "test-sa"},
		{in: "spiffe://xyz.com/wa/blabla/sa/test-sa/ns/default", expect: "test-sa"},
		{in: "spiffe://xyz.com/sa/test-sa/", expect: "test-sa"},
		{in: "spiffe://xyz.com/wa/blabla/sa/test-sa", expect: "test-sa"},
	}

	for _, c := range cases {
		actual := extractActualServiceAccount(c.in)
		if actual != c.expect {
			t.Errorf("%s: expecting %s, but got %s", c.in, c.expect, actual)
		}
	}
}

