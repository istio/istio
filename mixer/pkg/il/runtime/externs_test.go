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

package runtime

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestExternIp(t *testing.T) {
	b, err := externIp("1.2.3.4")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !bytes.Equal(b, net.ParseIP("1.2.3.4")) {
		t.Fatalf("Unexpected output: %v", b)
	}
}

func TestExternIp_Error(t *testing.T) {
	_, err := externIp("A.A.A.A")
	if err == nil {
		t.Fatalf("Expected error not found.")
	}
}

func TestExternIpEqual_True(t *testing.T) {
	b := externIpEqual(net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.4"))
	if !b {
		t.Fatal()
	}
}

func TestExternIpEqual_False(t *testing.T) {
	b := externIpEqual(net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.5"))
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
	}

	for _, c := range cases {
		if externMatch(c.s, c.p) != c.e {
			t.Fatalf("externMatch failure: %+v", c)
		}
	}
}
