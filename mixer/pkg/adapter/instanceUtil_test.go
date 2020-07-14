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

package adapter

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestStringify(t *testing.T) {
	testTime, _ := time.Parse("2006-Jan-02", "2013-Feb-03")
	tests := []struct {
		v    interface{}
		want string
	}{
		{v: "foo", want: "foo"},
		{v: int64(123456789), want: "123456789"},
		{v: float64(123456789.123456), want: "123456789.123456"},
		{v: true, want: "true"},
		{v: testTime, want: "2013-02-03T00:00:00.000000Z"},
		{v: 3 * time.Second, want: "3s"},
		{v: net.ParseIP("1.2.3.4"), want: "1.2.3.4"},
		{v: EmailAddress("abcd@abcd"), want: "abcd@abcd"},
		{v: URI("http://foo"), want: "http://foo"},
		{v: DNSName("dns"), want: "dns"},
		{v: nil, want: ""},
		{v: map[string]string{"a": "b"}, want: "a=b"},
		{v: map[string]string{"c": "d", "e": "f", "a": "b"}, want: "a=b&c=d&e=f"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("(%T) %v", tt.v, tt.v), func(t *testing.T) {
			got := Stringify(tt.v)
			if got != tt.want {
				t.Errorf("got \"%s\", want \"%s\"", got, tt.want)
			}
		})
	}
}

func TestStringEquals(t *testing.T) {
	tDuration, _ := time.ParseDuration("1s")
	tTime, _ := time.Parse("2006-Jan-02", "2013-Feb-03")
	t2Time, _ := time.Parse("2006-Jan-02", "2000-Feb-03")
	tests := []struct {
		a    interface{}
		b    interface{}
		want bool
	}{
		{a: nil, b: "", want: true},
		{a: "", b: nil, want: true},
		{a: nil, b: nil, want: true},

		{a: "foo", b: "foo", want: true},
		{a: "a", b: URI("a"), want: true},
		{a: URI("a"), b: URI("a"), want: true},
		{a: "a", b: DNSName("a"), want: true},
		{a: DNSName("a"), b: DNSName("a"), want: true},
		{a: "a", b: EmailAddress("a"), want: true},
		{a: EmailAddress("a"), b: EmailAddress("a"), want: true},
		{a: float64(123456789.123456), b: "123456789.123456", want: true},
		{a: float64(123456789.123456), b: float64(123456789.123456), want: true},
		{a: true, b: "true", want: true},
		{a: true, b: true, want: true},
		{a: int64(1234), b: "1234", want: true},
		{a: "1.2.3.4", b: net.ParseIP("1.2.3.4"), want: true},
		{a: net.ParseIP("1.2.3.4"), b: net.ParseIP("1.2.3.4"), want: true},
		{a: "1s", b: tDuration, want: true},
		{a: tDuration, b: tDuration, want: true},
		{a: "2013-02-03T00:00:00.000000Z", b: tTime, want: true},
		{a: tTime, b: tTime, want: true},
		{a: "a=b&c=d&e=f", b: map[string]string{"a": "b", "c": "d", "e": "f"}, want: true},
		{a: Stringify(map[string]string{"c": "d", "e": "f", "a": "b"}), b: map[string]string{"a": "b", "c": "d", "e": "f"}, want: true},
		{a: "1234", b: int(1234), want: true},

		{a: "foo", b: "bar", want: false},
		{a: float64(123456789.123456), b: float64(99999.123), want: false},
		{a: float64(123456789.123456), b: "99999.123456", want: false},
		{a: "a", b: EmailAddress("b"), want: false},
		{a: URI("a"), b: EmailAddress("b"), want: false},
		{a: net.ParseIP("1.2.3.4"), b: net.ParseIP("0.0.0.0"), want: false},
		{a: "badTimeFormat", b: tTime, want: false},
		{a: t2Time, b: tTime, want: false},
		{a: "a=b", b: map[string]string{"a": "b", "c": "d", "e": "f"}, want: false},
		{a: "a=b&c=d&o=o", b: map[string]string{"a": "b", "c": "d", "e": "f"}, want: false},
		{a: "***badmapstring***", b: map[string]string{"a": "b", "c": "d", "e": "f"}, want: false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T(%v),%T(%v)", tt.a, tt.a, tt.b, tt.b), func(t *testing.T) {
			got := StringEquals(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("got match=%v, want %v", got, tt.want)
			}
		})
	}
}
