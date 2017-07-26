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

import (
	"strings"
	"testing"
)

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

func TestFoldFind1(t *testing.T) {
	var tests = []struct {
		haystack string // input
		needle   string // input
		found    bool   // expected result
		offset   int    // where
	}{
		{"", "", true, 0},
		{"", "A", false, -1},
		{"abc", "", true, 0},
		{"abc", "ABCD", false, -1},
		{"abc", "ABC", true, 0},
		{"aBcd", "ABC", true, 0},
		{"xaBc", "ABC", true, 1},
		{"XYZaBcUVW", "Abc", true, 3},
		{"xaBcd", "ABC", true, 1},
		{"Xa", "A", true, 1},
		{"axabaBcd", "ABC", true, 4},
		{"axabxaBcd", "ABC", true, 5},
		{"axabxaBd", "ABC", false, -1},
		{"AAAAB", "AAAB", true, 1},
		{"xAAAxAAA", "AAAB", false, -1},
		{"xxxxAc", "AB", false, -1},
		{"X-: X", "-: ", true, 1},
		{"\nX", "*X", false, -1}, // \n shouldn't fold into *
		{"*X", "\nX", false, -1}, // \n shouldn't fold into *
		{"\rX", "-X", false, -1}, // \r shouldn't fold into -
		{"-X", "\rX", false, -1}, // \r shouldn't fold into -
		{"foo\r\nContent-Length: 34\r\n", "CONTENT-LENGTH:", true, 5},
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

func TestFoldFind2(t *testing.T) {
	var haystack [1]byte
	var needle [1]byte
	// we don't mind for these to map to eachother in exchange for 30% perf gain
	okExceptions := "@[\\]^_`{|}~"
	for i := 0; i < 127; i++ { // skipping 127 too, matches _
		haystack[0] = byte(i)
		for j := 0; j < 128; j++ {
			needle[0] = byte(j)
			sh := string(haystack[:])
			sn := string(needle[:])
			f, o := FoldFind(haystack[:], needle[:])
			shouldFind := strings.EqualFold(sh, sn)
			if i == j || shouldFind {
				if !f || o != 0 {
					t.Errorf("Not found when should: %d 0x%x '%s' matching %d 0x%x '%s'",
						i, i, sh, j, j, sn)
				}
				continue
			}
			if f || o != -1 {
				if strings.Contains(okExceptions, sh) {
					continue
				}
				t.Errorf("Found when shouldn't: %d 0x%x '%s' matching %d 0x%x '%s'",
					i, i, sh, j, j, sn)
			}
		}
	}
}

var utf8Str = "世界aBcdefGHiJklmnopqrstuvwxyZ"

func TestASCIIToUpper(t *testing.T) {
	SetLogLevel(Debug)
	var tests = []struct {
		input    string // input
		expected string // output
	}{
		{"", ""},
		{"A", "A"},
		{"aBC", "ABC"},
		{"AbC", "ABC"},
		{utf8Str, "\026LABCDEFGHIJKLMNOPQRSTUVWXYZ" /* got mangled but only first 2 */},
	}
	for _, tst := range tests {
		actual := ASCIIToUpper(tst.input)
		if tst.expected != string(actual) {
			t.Errorf("Got '%+v', expecting '%+v' for ASCIIFold('%s')", actual, tst.expected, tst.input)
		}
	}
	utf8bytes := []byte(utf8Str)
	if len(utf8bytes) != 26+6 {
		t.Errorf("Got %d utf8 bytes, expecting 6+26 for '%s'", len(utf8bytes), utf8Str)
	}
	folded := ASCIIToUpper(utf8Str)
	if len(folded) != 26+2 {
		t.Errorf("Got %d folded bytes, expecting 2+26 for '%s'", len(folded), utf8Str)
	}
}

func TestParseDecimal(t *testing.T) {
	var tests = []struct {
		input    string // input
		expected int    // output
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

func TestParseChunkSize(t *testing.T) {
	var tests = []struct {
		input     string // input
		expOffset int    // expected offset
		expValue  int    // expected value
	}{
		// Errors :
		{"", 0, -1},
		{"0", 1, -1},
		{"0\r", 2, -1},
		{"0\n", 2, -1},
		{"g\r\n", 0, -1},
		{"0\r0\n", 4, -1},
		// Ok: (size of input is the expected offset)
		{"0\r\n", 3, 0},
		{"0x\r\n", 4, 0},
		{"f\r\n", 3, 15},
		{"10\r\n", 4, 16},
		{"fF\r\n", 4, 255},
		{"abcdef\r\n", 8, 0xabcdef},
		{"100; foo bar\r\nanother line\r\n", 14 /* and not the whole thing */, 256},
	}
	for _, tst := range tests {
		actOffset, actVal := ParseChunkSize([]byte(tst.input))
		if tst.expValue != actVal {
			t.Errorf("Got %d, expecting %d for value of ParseChunkSize('%+s')", actVal, tst.expValue, tst.input)
		}
		if tst.expOffset != actOffset {
			t.Errorf("Got %d, expecting %d for offset of ParseChunkSize('%+s')", actOffset, tst.expOffset, tst.input)
		}
	}
}

func TestDebugSummary(t *testing.T) {
	var tests = []struct {
		input    string
		expected string
	}{
		{"12345678", "12345678"},
		{"123456789", "123456789"},
		{"1234567890", "1234567890"},
		{"12345678901", "12345678901"},
		{"123456789012", "12: 1234...9012"},
		{"1234567890123", "13: 1234...0123"},
		{"12345678901234", "14: 1234...1234"},
		{"A\r\000\001\x80\nB", `A\r\x00\x01\x80\nB`},                   // escaping
		{"A\r\000Xyyyyyyyyy\001\x80\nB", `17: A\r\x00X...\x01\x80\nB`}, // escaping
	}
	for _, tst := range tests {
		if actual := DebugSummary([]byte(tst.input), 8); actual != tst.expected {
			t.Errorf("Got '%s', expected '%s' for DebugSummary(%q)", actual, tst.expected, tst.input)
		}
	}
}

// --- for bench mark/comparaison

func asciiFold0(str string) []byte {
	return []byte(strings.ToUpper(str))
}

var toLowerMaskRune = rune(toUpperMask)

func toLower(r rune) rune {
	return r & toLowerMaskRune
}

func asciiFold1(str string) []byte {
	return []byte(strings.Map(toLower, str))
}

var lw []byte

func BenchmarkASCIIFoldNormalToLower(b *testing.B) {
	for n := 0; n < b.N; n++ {
		lw = asciiFold0(utf8Str)
	}
}
func BenchmarkASCIIFoldCustomToLowerMap(b *testing.B) {
	for n := 0; n < b.N; n++ {
		lw = asciiFold1(utf8Str)
	}
}

// Package's version (3x fastest)
func BenchmarkASCIIToUpper(b *testing.B) {
	SetLogLevel(Warning)
	for n := 0; n < b.N; n++ {
		lw = ASCIIToUpper(utf8Str)
	}
}

// Note: newline inserted in set-cookie line because of linter (line too long)
var testHaystack = []byte(`HTTP/1.1 200 OK
Date: Sun, 16 Jul 2017 21:00:29 GMT
Expires: -1
Cache-Control: private, max-age=0
Content-Type: text/html; charset=ISO-8859-1
P3P: CP="This is not a P3P policy! See https://www.google.com/support/accounts/answer/151657?hl=en for more info."
Server: gws
X-XSS-Protection: 1; mode=block
X-Frame-Options: SAMEORIGIN
Set-Cookie: NID=107=sne5itxJgY_4dD951psa7cyP_rQ3ju-J9p0QGmKYl0l0xUVSVmGVeX8smU0VV6FyfQnZ4kkhaZ9ozxLpUWH-77K_0W8aXzE3
PDQxwAynvJgGGA9rMRB9bperOblUOQ3XilG6B5-8auMREgbc; expires=Mon, 15-Jan-2018 21:00:29 GMT; path=/; domain=.google.com; HttpOnly
Accept-Ranges: none
Vary: Accept-Encoding
Transfer-Encoding: chunked
`)

func FoldFind0(haystack []byte, needle []byte) (bool, int) {
	offset := strings.Index(strings.ToUpper(string(haystack)), string(needle))
	found := (offset >= 0)
	return found, offset
}

func BenchmarkFoldFind0(b *testing.B) {
	needle := []byte("VARY")
	for n := 0; n < b.N; n++ {
		FoldFind0(testHaystack, needle)
	}
}

func BenchmarkFoldFind(b *testing.B) {
	needle := []byte("VARY")
	for n := 0; n < b.N; n++ {
		FoldFind(testHaystack, needle)
	}
}
