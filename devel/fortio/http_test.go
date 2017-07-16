// Copyright 2017 Istio Authors
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

package fortio

import "testing"

func TestNewHTTPRequest(t *testing.T) {
	var tests = []struct {
		url string // input
		ok  bool   // ok/error
	}{
		{"http://www.google.com/", true},
		{"ht tp://www.google.com/", false},
	}
	for _, tst := range tests {
		r := newHTTPRequest(tst.url)
		if tst.ok != (r != nil) {
			t.Errorf("Got %v, expecting ok %v for url '%s'", r, tst.ok, tst.url)
		}
	}
}

func TestFoldFind(t *testing.T) {
	var tests = []struct {
		haystack string // input
		needle   string // input
		found    bool   // expected result
		offset   int    // where
	}{
		{"", "", true, 0},
		{"", "a", false, -1},
		{"abc", "", true, 0},
		{"abc", "abcd", false, -1},
		{"abc", "abc", true, 0},
		{"aBcd", "abc", true, 0},
		{"xaBc", "abc", true, 1},
		{"aBc", "Abc", false, -1}, // only the haystack is folded
		{"xaBcd", "abc", true, 1},
		{"xA", "a", true, 1},
		{"axabaBcd", "abc", true, 4},
		{"axabxaBcd", "abc", true, 5},
		{"axabxaBd", "abc", false, -1},
	}
	for _, tst := range tests {
		f, o := FoldFind([]byte(tst.haystack), []byte(tst.needle))
		if tst.found != f {
			t.Errorf("Got %v, expecting found %v for FoldFind('%s', '%s')", f, tst.found, tst.haystack, tst.needle)
		}
		if tst.offset != o {
			t.Errorf("Offset %d, expecting %d for FoldFind('%s', '%s')", o, tst.offset, tst.haystack, tst.needle)
		}
	}
}

func TestASCIIFold(t *testing.T) {
	utf8Str := "世界aBc"
	var tests = []struct {
		input    string // input
		expected string // input
	}{
		{"", ""},
		{"a", "a"},
		{"aBC", "abc"},
		{"AbC", "abc"},
		{utf8Str, "6labc" /* got mangled but only first 2 */},
	}
	for _, tst := range tests {
		actual := ASCIIFold(tst.input)
		if tst.expected != string(actual) {
			t.Errorf("Got '%+v', expecting '%+v' for ASCIIFold('%s')", actual, tst.expected, tst.input)
		}
	}
	utf8bytes := []byte(utf8Str)
	if len(utf8bytes) != 9 {
		t.Errorf("Got %d utf8 bytes, expecting 9 for '%s'", len(utf8bytes), utf8Str)
	}
	folded := ASCIIFold(utf8Str)
	if len(folded) != 5 {
		t.Errorf("Got %d folded bytes, expecting 2+3 for '%s'", len(folded), utf8Str)
	}
}

func TestParseDecimal(t *testing.T) {
	var tests = []struct {
		input    string // input
		expected int    // input
	}{
		{"", -1},
		{"3", 3},
		{" 456cxzc", 456},
		{"-45", -1}, // - is not expected, positive numbers only
		{"3.2", 3},  // stops at first non digit
		{"    1 2", 1},
		{"0", 0},
	}
	for _, tst := range tests {
		actual := ParseDecimal([]byte(tst.input))
		if tst.expected != actual {
			t.Errorf("Got %d, expecting %d for ParseDecimal('%s')", actual, tst.expected, tst.input)
		}
	}
}
