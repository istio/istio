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

package trustdomain

import (
	"reflect"
	"testing"
)

func TestReplaceTrustDomainAliases(t *testing.T) {
	type inStruct struct {
		trustDomainBundle TrustDomainBundle
		users             []string
	}
	testCases := []struct {
		name   string
		in     inStruct
		expect []string
	}{
		{
			name: "No trust domain aliases (no change in trust domain)",
			in: inStruct{NewTrustDomainBundle("cluster.local", nil),
				[]string{"cluster.local/ns/foo/sa/bar"}},
			expect: []string{"cluster.local/ns/foo/sa/bar"},
		},
		{
			name: "One trust domain alias, one principal",
			in: inStruct{NewTrustDomainBundle("td2", []string{"td1"}),
				[]string{"td1/ns/foo/sa/bar"}},
			expect: []string{"td2/ns/foo/sa/bar", "td1/ns/foo/sa/bar"},
		},
		{
			name: "One trust domain alias, two principals",
			in: inStruct{NewTrustDomainBundle("td1", []string{"cluster.local"}),
				[]string{"cluster.local/ns/foo/sa/bar", "cluster.local/ns/yyy/sa/zzz"}},
			expect: []string{"td1/ns/foo/sa/bar", "cluster.local/ns/foo/sa/bar", "td1/ns/yyy/sa/zzz", "cluster.local/ns/yyy/sa/zzz"},
		},
		{
			name: "Two trust domain aliases, two principals",
			in: inStruct{NewTrustDomainBundle("td2", []string{"td1", "cluster.local"}),
				[]string{"cluster.local/ns/foo/sa/bar", "td1/ns/yyy/sa/zzz"}},
			expect: []string{"td2/ns/foo/sa/bar", "td1/ns/foo/sa/bar", "cluster.local/ns/foo/sa/bar",
				"td2/ns/yyy/sa/zzz", "td1/ns/yyy/sa/zzz", "cluster.local/ns/yyy/sa/zzz"},
		},
	}

	for _, tc := range testCases {
		got := tc.in.trustDomainBundle.ReplaceTrustDomainAliases(tc.in.users)
		if !reflect.DeepEqual(got, tc.expect) {
			t.Errorf("%s failed. Expect: %s. Got: %s", tc.name, tc.expect, got)
		}
	}
}

func TestReplaceTrustDomainInPrincipal(t *testing.T) {
	cases := []struct {
		trustDomainIn string
		principal     string
		out           string
	}{
		{principal: "spiffe://cluster.local/ns/foo/sa/bar", out: ""},
		{principal: "sa/test-sa/ns/default", out: ""},
		{trustDomainIn: "td", principal: "cluster.local/ns/foo/sa/bar", out: "td/ns/foo/sa/bar"},
		{trustDomainIn: "abc", principal: "xyz/ns/foo/sa/bar", out: "abc/ns/foo/sa/bar"},
	}

	for _, c := range cases {
		got := replaceTrustDomainInPrincipal(c.trustDomainIn, c.principal)
		if got != c.out {
			t.Errorf("expect %s, but got %s", c.out, got)
		}
	}
}

func TestGetTrustDomain(t *testing.T) {
	cases := []struct {
		principal string
		out       string
	}{
		{principal: "spiffe://cluster.local/ns/foo/sa/bar", out: ""},
		{principal: "sa/test-sa/ns/default", out: ""},
		{principal: "cluster.local/ns/foo/sa/bar", out: "cluster.local"},
		{principal: "xyz/ns/foo/sa/bar", out: "xyz"},
	}

	for _, c := range cases {
		got := getTrustDomain(c.principal)
		if got != c.out {
			t.Errorf("expect %s, but got %s", c.out, got)
		}
	}
}
