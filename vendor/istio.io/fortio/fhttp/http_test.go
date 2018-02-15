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

package fhttp

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"istio.io/fortio/log"
)

func init() {
	log.SetLogLevel(log.Debug)
}

func TestGetHeaders(t *testing.T) {
	o := NewHTTPOptions("")
	o.AddAndValidateExtraHeader("FOo:baR")
	oo := *o // check that copying works
	h := oo.GetHeaders()
	if len(h) != 2 { // 1 above + user-agent
		t.Errorf("Header count mismatch, got %d instead of 3", len(h))
	}
	if h.Get("Foo") != "baR" {
		t.Errorf("Foo header mismatch, got '%v'", h.Get("Foo"))
	}
	if h.Get("Host") != "" {
		t.Errorf("Host header should be nil initially, got '%v'", h.Get("Host"))
	}
	o.AddAndValidateExtraHeader("hoSt:   aBc:123")
	h = o.GetHeaders()
	if h.Get("Host") != "aBc:123" {
		t.Errorf("Host header mismatch, got '%v'", h.Get("Host"))
	}
	if len(h) != 3 { // 2 above + user-agent
		t.Errorf("Header count mismatch, got %d instead of 3", len(h))
	}
	err := o.AddAndValidateExtraHeader("foo") // missing : value
	if err == nil {
		t.Errorf("Expected error for header without value, did not get one")
	}
	o.ResetHeaders()
	h = o.GetHeaders()
	if h.Get("Host") != "" {
		t.Errorf("After reset Host header should be nil, got '%v'", h.Get("Host"))
	}
	if len(h) != 0 {
		t.Errorf("Header count mismatch after reset, got %d instead of 1", len(h))
	}
}

func TestNewHTTPRequest(t *testing.T) {
	var tests = []struct {
		url string // input
		ok  bool   // ok/error
	}{
		{"http://www.google.com/", true},
		{"ht tp://www.google.com/", false},
	}
	for _, tst := range tests {
		o := NewHTTPOptions(tst.url)
		o.AddAndValidateExtraHeader("Host: www.google.com")
		r := newHTTPRequest(o)
		if tst.ok != (r != nil) {
			t.Errorf("Got %v, expecting ok %v for url '%s'", r, tst.ok, tst.url)
		}
	}
}

func TestMultiInitAndEscape(t *testing.T) {
	// one escaped already 2 not
	o := NewHTTPOptions("localhost:8080/?delay=1s:10%,0.5s:15%25,0.25s:5%")
	expected := "http://localhost:8080/?delay=1s:10%25,0.5s:15%25,0.25s:5%25"
	if o.URL != expected {
		t.Errorf("Got initially '%s', expected '%s'", o.URL, expected)
	}
	o.AddAndValidateExtraHeader("FoO: BaR")
	// re init should not erase headers
	o.Init(o.URL)
	if o.GetHeaders().Get("Foo") != "BaR" {
		t.Errorf("Lost header after Init %+v", o.GetHeaders())
	}
	// Escaping should be indempotent
	if o.URL != expected {
		t.Errorf("Got after reinit '%s', expected '%s'", o.URL, expected)
	}
}

func TestSchemeCheck(t *testing.T) {
	var tests = []struct {
		input  string
		output string
		stdcli bool
	}{
		{"https://www.google.com/", "https://www.google.com/", true},
		{"www.google.com", "http://www.google.com", false},
		{"hTTps://foo.bar:123/ab/cd", "hTTps://foo.bar:123/ab/cd", true}, // not double http:
		{"HTTP://foo.bar:124/ab/cd", "HTTP://foo.bar:124/ab/cd", false},  // not double http:
		{"", "", false},                      // and error in the logs
		{"x", "http://x", false},             //should not crash because url is shorter than prefix
		{"http:/", "http://http:/", false},   //boundary
		{"http://", "http://", false},        //boundary
		{"https://", "https://", true},       //boundary
		{"https:/", "http://https:/", false}, //boundary
	}
	for _, tst := range tests {
		o := NewHTTPOptions(tst.input)
		if o.URL != tst.output {
			t.Errorf("Got %v, expecting %v for url '%s'", o.URL, tst.output, tst.input)
		}
		if o.DisableFastClient != tst.stdcli {
			t.Errorf("Got %v, expecting %v for stdclient for url '%s'", o.DisableFastClient, tst.stdcli, tst.input)
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
	log.SetLogLevel(log.Debug)
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

func TestParseStatus(t *testing.T) {
	var tests = []struct {
		input    string
		expected int
	}{
		// Error cases
		{"x", 400},
		{"1::", 400},
		{"x:10", 400},
		{"555:-1", 400},
		{"555:101", 400},
		{"551:45,551:56", 400},
		// Good cases
		{"555", 555},
		{"555:100", 555},
		{"555:100%", 555},
		{"555:0", 200},
		{"555:0%", 200},
		{"551:45,551:55", 551},
		{"551:45%,551:55%", 551},
	}
	for _, tst := range tests {
		if actual := generateStatus(tst.input); actual != tst.expected {
			t.Errorf("Got %d, expected %d for generateStatus(%q)", actual, tst.expected, tst.input)
		}
	}
}

func TestParseDelay(t *testing.T) {
	var tests = []struct {
		input    string
		expected time.Duration
	}{
		// Error cases
		{"", -1},
		{"x", -1},
		{"1::", -1},
		{"x:10", -1},
		{"10ms:-1", -1},
		{"20ms:101", -1},
		{"20ms:101%", -1},
		{"10ms:45,100ms:56", -1},
		// Max delay case:
		{"10s:45,10s:55", 1 * time.Second},
		// Good cases
		{"100ms", 100 * time.Millisecond},
		{"100ms:100", 100 * time.Millisecond},
		{"100ms:100%", 100 * time.Millisecond},
		{"100ms:0", 0},
		{"100ms:0%", 0},
		{"10ms:45,10ms:55", 10 * time.Millisecond},
		{"10ms:45%,10ms:55%", 10 * time.Millisecond},
	}
	for _, tst := range tests {
		if actual := generateDelay(tst.input); actual != tst.expected {
			t.Errorf("Got %d, expected %d for generateStatus(%q)", actual, tst.expected, tst.input)
		}
	}
}

func TestGenerateStatusBasic(t *testing.T) {
	var tests = []struct {
		input    string
		expected int
	}{
		// Error cases
		{"x", 400},
		{"1::", 400},
		{"x:10", 400},
		{"555:x", 400},
		{"555:-1", 400},
		{"555:101", 400},
		{"551:45,551:56", 400},
		// Good cases
		{"555", 555},
		{"555:100", 555},
		{"555:0", 200},
		{"551:45,551:55", 551},
	}
	for _, tst := range tests {
		if actual := generateStatus(tst.input); actual != tst.expected {
			t.Errorf("Got %d, expected %d for generateStatus(%q)", actual, tst.expected, tst.input)
		}
	}
}

func TestGenerateStatusEdgeSum(t *testing.T) {
	st := "503:99.0,503:1.00001"
	// Gets 400 without rounding as it exceeds 100, another corner case is if you
	// add 0.1 1000 times you get 0.99999... so you may get stray 200s without Rounding
	if actual := generateStatus(st); actual != 503 {
		t.Errorf("Got %d for generateStatus(%q)", actual, st)
	}
	st += ",500:0.0001"
	if actual := generateStatus(st); actual != 400 {
		t.Errorf("Got %d for long generateStatus(%q) when expecting 400 for > 100", actual, st)
	}
}

// Round down to the nearest thousand
func roundthousand(x int) int {
	return int(float64(x)+500.) / 1000
}

func TestGenerateStatusDistribution(t *testing.T) {
	log.SetLogLevel(log.Info)
	str := "501:20,502:30,503:0.5"
	m := make(map[int]int)
	for i := 0; i < 10000; i++ {
		m[generateStatus(str)]++
	}
	if len(m) != 4 {
		t.Errorf("Unexpected result, expecting 4 statuses, got %+v", m)
	}
	if m[200]+m[501]+m[502]+m[503] != 10000 {
		t.Errorf("Unexpected result, expecting 4 statuses summing to 10000 got %+v", m)
	}
	if m[503] <= 10 {
		t.Errorf("Unexpected result, expecting at least 10 count for 0.5%% probability over 10000 got %+v", m)
	}
	// Round the data
	f01 := roundthousand(m[501]) // 20% -> 2
	f02 := roundthousand(m[502]) // 30% -> 3
	fok := roundthousand(m[200]) // rest is 50% -> 5
	f03 := roundthousand(m[503]) // 0.5% -> rounds down to 0 10s of %

	if f01 != 2 || f02 != 3 || fok != 5 || (f03 != 0) {
		t.Errorf("Unexpected distribution for %+v - wanted 2 3 5, got %d %d %d", m, f01, f02, fok)
	}
}

func TestRoundDuration(t *testing.T) {
	var tests = []struct {
		input    time.Duration
		expected time.Duration
	}{
		{0, 0},
		{1200 * time.Millisecond, 1200 * time.Millisecond},
		{1201 * time.Millisecond, 1200 * time.Millisecond},
		{1249 * time.Millisecond, 1200 * time.Millisecond},
		{1250 * time.Millisecond, 1300 * time.Millisecond},
		{1299 * time.Millisecond, 1300 * time.Millisecond},
	}
	for _, tst := range tests {
		if actual := RoundDuration(tst.input); actual != tst.expected {
			t.Errorf("Got %v, expected %v for RoundDuration(%v)", actual, tst.expected, tst.input)
		}
	}
}

// Many of the earlier http tests are through httprunner but new tests should go here

func TestEchoBack(t *testing.T) {
	p, m := DynamicHTTPServer(false)
	m.HandleFunc("/", EchoHandler)
	v := url.Values{}
	v.Add("foo", "bar")
	url := fmt.Sprintf("http://localhost:%d/", p)
	resp, err := http.PostForm(url, v)
	if err != nil {
		t.Fatalf("post form err %v", err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readall err %v", err)
	}
	expected := "foo=bar"
	if string(b) != expected {
		t.Errorf("Got %s while expected %s", DebugSummary(b, 128), expected)
	}
}

func TestInvalidRequest(t *testing.T) {
	o := HTTPOptions{
		URL: "http://www.google.com/", // valid url
	}
	client := NewStdClient(&o)
	client.ChangeURL(" http://bad.url.with.space.com/") // invalid url
	// should not crash (issue #93), should error out
	code, _, _ := client.Fetch()
	if code != http.StatusBadRequest {
		t.Errorf("Got %d code while expecting bad request (%d)", code, http.StatusBadRequest)
	}
	o.URL = client.url
	c2 := NewStdClient(&o)
	if c2 != nil {
		t.Errorf("Got non nil client %+v code while expecting nil for bad request", c2)
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
	log.SetLogLevel(log.Warning)
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

// -- benchmarks --

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
