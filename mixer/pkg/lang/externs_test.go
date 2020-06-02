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

package lang

import (
	"bytes"
	"net"
	"testing"
	"time"

	"istio.io/pkg/attribute"
)

func TestExternIp(t *testing.T) {
	b, err := ExternIP("1.2.3.4")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !bytes.Equal(b, net.ParseIP("1.2.3.4")) {
		t.Fatalf("Unexpected output: %v", b)
	}
}

func TestExternIp_Error(t *testing.T) {
	_, err := ExternIP("A.A.A.A")
	if err == nil {
		t.Fatalf("Expected error not found.")
	}
}

func TestExternIpEqual_True(t *testing.T) {
	b := ExternIPEqual(net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.4"))
	if !b {
		t.Fatal()
	}
}

func TestExternIpEqual_False(t *testing.T) {
	b := ExternIPEqual(net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.5"))
	if b {
		t.Fatal()
	}
}

func TestExternTimestamp(t *testing.T) {
	ti, err := externTimestamp("2015-01-02T15:04:35Z")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if ti.Year() != 2015 || ti.Month() != time.January || ti.Day() != 2 || ti.Hour() != 15 || ti.Minute() != 4 {
		t.Fatalf("Unexpected time: %v", ti)
	}
}

func TestExternTimestamp_Error(t *testing.T) {
	_, err := externTimestamp("AAA")
	if err == nil {
		t.Fatalf("Expected error not found.")
	}
}

func TestExternTimestampEqual_True(t *testing.T) {
	t1, _ := externTimestamp("2015-01-02T15:04:35Z")
	t2, _ := externTimestamp("2015-01-02T15:04:35Z")
	b := externTimestampEqual(t1, t2)
	if !b {
		t.Fatal()
	}
}

func TestExternTimestampEqual_False(t *testing.T) {
	t1, _ := externTimestamp("2015-01-02T15:04:35Z")
	t2, _ := externTimestamp("2018-11-11T15:04:35Z")
	b := externTimestampEqual(t1, t2)
	if b {
		t.Fatal()
	}
}

func TestExternDnsName_Positive(t *testing.T) {
	positive := []string{
		"foo",
		"foo.",
		"fOO",
		"Foo",
		"f00",
		"f00.",
		"a-b.c-d",
	}

	for _, v := range positive {
		o, err := ExternDNSName(v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if o != v {
			t.Fatalf("%v != %v", o, v)
		}
	}
}

func TestExternDnsName_Negative(t *testing.T) {
	negative := []string{
		"",
		".",
		"b..a",
		"b..a.",
		"-a",
		"b.-a.c",
		"f-",
		"f-.",
		"f-.boo",
		"f--.boo",
	}

	for _, v := range negative {
		_, err := ExternDNSName(v)
		if err == nil {
			t.Fatalf("Expected error not found for: %v", v)
		}
	}
}

func TestExternDnsNameEqual_Positive(t *testing.T) {
	positive := map[string]string{
		"foo":     "foo",
		"FOO":     "foo",
		"foo.":    "foo",
		"boo":     "boo.",
		"zoo.":    "zoo.",
		"foo.bar": "foo.bar",
	}

	for k, v := range positive {
		eq, err := ExternDNSNameEqual(k, v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if !eq {
			t.Fatalf("Expected to be equal: %s != %s", k, v)
		}
	}
}

func TestExternDnsNameEqual_Negative(t *testing.T) {
	negative := map[string]string{
		"foo.bar-a.": "foo.bar",
	}

	for k, v := range negative {
		eq, err := ExternDNSNameEqual(k, v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if eq {
			t.Fatalf("Expected to be not equal: %s != %s", k, v)
		}
	}
}

func TestExternDnsNameEqual_Error(t *testing.T) {
	erroneous := map[string]string{
		"a-": "a",
		"b":  "b-",
	}

	for k, v := range erroneous {
		_, err := ExternDNSNameEqual(k, v)
		if err == nil {
			t.Fatalf("Expected error not found for: %v == %v", k, v)
		}
	}
}

func TestExternEmail_Positive(t *testing.T) {
	positive := []string{
		"foo@bar",
		"foo@bar.com",
		"foo+a@bar.com",
		`"john..doe"@bar.com`,
	}

	for _, v := range positive {
		o, err := ExternEmail(v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if o != v {
			t.Fatalf("%v != %v", o, v)
		}
	}
}

func TestExternEmail_Negative(t *testing.T) {
	negative := []string{
		"foo",
		"foo.",
		"foo@",
		"@foo",
		"foo+a@bar-.com",
		`aaaa <johndoe@bar.com>`,
	}

	for _, v := range negative {
		_, err := ExternEmail(v)
		if err == nil {
			t.Fatalf("Expected error not found for: %v", v)
		}
	}
}

func TestExternEmailEqual_Positive(t *testing.T) {
	positive := map[string]string{
		"foo@bar.com":   "foo@bar.com",
		"foo@Bar.com":   "foo@bar.com",
		`"foo"@bar.com`: "foo@bar.com",
	}

	for k, v := range positive {
		eq, err := ExternEmailEqual(k, v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if !eq {
			t.Fatalf("Expected to be equal: %s != %s", k, v)
		}
	}
}

func TestExternEmailEqual_Error(t *testing.T) {
	erroneous := map[string]string{
		"foo..baz@bar.com": "foo@bar.com",
		"foo@bar.com":      "foo..baz@bar.com",
		"foo":              "foo@bar.com",
		"boo@bar.com":      "foo",
	}

	for k, v := range erroneous {
		_, err := ExternEmailEqual(k, v)
		if err == nil {
			t.Fatalf("Expected error not found for: %v == %v", k, v)
		}
	}
}

func TestExternEmailEqual_Negative(t *testing.T) {
	negative := map[string]string{
		"foo@bar.com": "foo@foo.com",
		"foo@foo.com": "bar@foo.com",
		"Foo@bar.com": "foo@bar.com",
	}

	for k, v := range negative {
		eq, err := ExternEmailEqual(k, v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if eq {
			t.Fatalf("Expected to be not equal: %s != %s", k, v)
		}
	}
}

func TestGetEmailParts(t *testing.T) {
	cases := [][]string{
		{"", "", ""},
		{"@", "", ""},
		{"foo", "foo", ""},
		{"foo@", "foo", ""},
		{"@foo", "", "foo"},
		{"foo@bar", "foo", "bar"},
		{"foo@bar@baz", "foo", "bar@baz"},
	}

	for _, c := range cases {
		p1, p2 := getEmailParts(c[0])
		if p1 != c[1] || p2 != c[2] {
			t.Fatalf("Failed for '%s' got: '%s','%s' wanted: '%s','%s'.", c[0], p1, p2, c[1], c[2])
		}
	}
}

func TestExternUri_Positive(t *testing.T) {
	positive := []string{
		"urn:foo",
		"urn:foo/bar",
		"urn:foo/bar?q=5",
		"urn:foo/bar?q=5#frr",
		"http://foo",
		"http://foo/",
		"http://foo/bar",
		"http://foo/bar/",
		"http://foo/bar/?q=4",
		"http://foo/bar/?q=4#fr",
		"https://foo/bar/?q=4#fr",
		"ftp://foo/bar/?q=4#fr",
		"fs:foo",
		"fs:/foo",
		"foo",
		"http://[fe80::1%25en0]",
	}

	for _, v := range positive {
		o, err := ExternURI(v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if o != v {
			t.Fatalf("%v != %v", o, v)
		}
	}
}

func TestExternUri_Negative(t *testing.T) {
	negative := []string{
		"",
		":/",
		":a",
		`http://[fe80::1%25en0`,
	}

	for _, v := range negative {
		_, err := ExternURI(v)
		if err == nil {
			t.Fatalf("Expected error not found for: '%v'", v)
		}
	}
}

func TestExternUriEqual_Positive(t *testing.T) {
	positive := map[string]string{
		"foo":                "foo",
		"scheme:bar":         "scheme:bar",
		"urn:bar":            "URN:bar",
		"http://host":        "http://host",
		"http://HOST":        "http://host",
		"http://HOST.com":    "http://host.COM",
		"http://HOST.com:80": "http://host.COM:80",
		"http://HOST.COM:":   "http://host.COM",
		"HTTP://host.com:":   "http://host.com",
	}

	for k, v := range positive {
		eq, err := ExternURIEqual(k, v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if !eq {
			t.Fatalf("Expected to be equal: %s != %s", k, v)
		}
	}
}

func TestExternUriEqual_Negative(t *testing.T) {
	negative := map[string]string{
		"urn:foo":         "urn:bar",
		"urn:BAR":         "urn:bar",
		"http://host":     "http://host/",
		"http://bar":      "http://host",
		"http://host:80":  "http://host:81",
		"http://host.com": "https://host.com",
	}

	for k, v := range negative {
		eq, err := ExternURIEqual(k, v)
		if err != nil {
			t.Fatalf("Error: %v for %s", err, v)
		}
		if eq {
			t.Fatalf("Expected to be not equal: %s != %s", k, v)
		}
	}
}

func TestExternUriEqual_Error(t *testing.T) {
	erroneous := map[string]string{
		":/": "a",
		"a":  ":/",
	}

	for k, v := range erroneous {
		_, err := ExternURIEqual(k, v)
		if err == nil {
			t.Fatalf("Expected error not found for: %v == %v", k, v)
		}
	}
}

func TestExternMatch(t *testing.T) {
	var cases = []struct {
		s string
		p string
		e bool
	}{
		{"ns1.svc.local", "ns1.*", true},
		{"ns1.svc.local", "ns2.*", false},
		{"svc1.ns1.cluster", "*.ns1.cluster", true},
		{"svc1.ns1.cluster", "*.ns1.cluster1", false},
		{"svc1.ns1.cluster", "svc1.ns1.cluster", true},
		{"svc1.ns1.cluster", "svc2.ns1.cluster", false},
	}

	for _, c := range cases {
		if ExternMatch(c.s, c.p) != c.e {
			t.Fatalf("externMatch failure: %+v", c)
		}
	}
}

func TestExternMatches(t *testing.T) {
	var cases = []struct {
		p string
		s string
		e bool
	}{
		{"ns1\\.svc\\.local", "ns1.svc.local", true},
		{"ns1.*", "ns1.svc.local", true},
		{"ns2.*", "ns1.svc.local", false},
	}

	for _, c := range cases {
		if m, err := externMatches(c.p, c.s); err != nil {
			t.Errorf("Unexpected error: %+v, %v", c, err)
		} else if m != c.e {
			t.Errorf("matches failure: %+v", c)
		}
	}
}

func TestExternStartsWith(t *testing.T) {
	var cases = []struct {
		s string
		p string
		e bool
	}{
		{"abc", "a", true},
		{"abc", "", true},
		{"abc", "abc", true},
		{"abc", "abcd", false},
		{"cba", "a", false},
	}

	for _, c := range cases {
		m := ExternStartsWith(c.s, c.p)
		if m != c.e {
			t.Fatalf("startsWith failure: %+v", c)
		}
	}
}

func TestExternEndsWith(t *testing.T) {
	var cases = []struct {
		s string
		u string
		e bool
	}{
		{"abc", "c", true},
		{"abc", "", true},
		{"abc", "abc", true},
		{"abc", "dabc", false},
		{"cba", "c", false},
	}

	for _, c := range cases {
		m := ExternEndsWith(c.s, c.u)
		if m != c.e {
			t.Fatalf("endsWith failure: %+v", c)
		}
	}
}

func TestExternEmptyStringMap(t *testing.T) {
	m := externEmptyStringMap()
	if !attribute.Equal(m, attribute.WrapStringMap(nil)) {
		t.Errorf("emptyStringMap() returned non-empty map: %#v", m)
	}
}

func TestExternConditionalString(t *testing.T) {
	got := externConditionalString(true, "yes", "no")
	if got != "yes" {
		t.Errorf("externIfElse(true, \"yes\", \"no\") => %s, wanted: yes", got)
	}
}
