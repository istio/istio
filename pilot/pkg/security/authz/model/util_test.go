// Copyright Istio Authors
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
	"strings"
	"testing"
)

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

func TestExtractNameInBrackets(t *testing.T) {
	cases := []struct {
		s    string
		want string
		err  bool
	}{
		{s: "[good]", want: "good"},
		{s: "[[good]]", want: "[good]"},
		{s: "[]", want: ""},
		{s: "[bad", err: true},
		{s: "bad]", err: true},
		{s: "bad", err: true},
	}

	for _, c := range cases {
		t.Run(c.s, func(t *testing.T) {
			s, err := extractNameInBrackets(c.s)
			if s != c.want {
				t.Errorf("want %s but found %s", c.want, s)
			}
			if c.err != (err != nil) {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestExtractNameInNestedBrackets(t *testing.T) {
	cases := []struct {
		s    string
		want []string
		err  bool
	}{
		{s: "[good]", want: []string{"good"}},
		{s: "[good][abc][xyz]", want: []string{"good", "abc", "xyz"}},
		{s: "[]", want: []string{""}},
		{s: "[[good]", want: []string{"[good"}},
		{s: "[good]]", want: []string{"good]"}},
		{s: "[[good]]", want: []string{"[good]"}},
		{s: "x[bad]", err: true},
		{s: "[bad", err: true},
		{s: "bad]", err: true},
		{s: "bad", err: true},
	}

	for _, c := range cases {
		t.Run(c.s, func(t *testing.T) {
			s, err := extractNameInNestedBrackets(c.s)
			if !reflect.DeepEqual(s, c.want) {
				t.Errorf("want %s but found %s", c.want, s)
			}
			if c.err != (err != nil) {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
