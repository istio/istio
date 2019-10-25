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
	testCases := []struct {
		name              string
		trustDomainBundle Bundle
		principals        []string
		expect            []string
	}{
		{
			name:              "No trust domain aliases (no change in trust domain)",
			trustDomainBundle: NewTrustDomainBundle("cluster.local", nil),
			principals:        []string{"cluster.local/ns/foo/sa/bar"},
			expect:            []string{"cluster.local/ns/foo/sa/bar"},
		},
		{
			name:              "Principal with *",
			trustDomainBundle: NewTrustDomainBundle("cluster.local", nil),
			principals:        []string{"*"},
			expect:            []string{"*"},
		},
		{
			name:              "Principal with * prefix and right format",
			trustDomainBundle: NewTrustDomainBundle("cluster.local", nil),
			principals:        []string{"*/ns/foo/sa/bar"},
			expect:            []string{"*/ns/foo/sa/bar"},
		},
		{
			name:              "One trust domain alias, one principal",
			trustDomainBundle: NewTrustDomainBundle("td2", []string{"td1"}),
			principals:        []string{"td1/ns/foo/sa/bar"},
			expect:            []string{"td2/ns/foo/sa/bar", "td1/ns/foo/sa/bar"},
		},
		{
			name:              "One trust domain alias, two principals",
			trustDomainBundle: NewTrustDomainBundle("td1", []string{"cluster.local"}),
			principals:        []string{"cluster.local/ns/foo/sa/bar", "cluster.local/ns/yyy/sa/zzz"},
			expect:            []string{"td1/ns/foo/sa/bar", "cluster.local/ns/foo/sa/bar", "td1/ns/yyy/sa/zzz", "cluster.local/ns/yyy/sa/zzz"},
		},
		{
			name:              "One trust domain alias, principals with * as-is",
			trustDomainBundle: NewTrustDomainBundle("td1", []string{"cluster.local"}),
			principals:        []string{"*/ns/foo/sa/bar", "*sa/zzz", "*"},
			expect:            []string{"*/ns/foo/sa/bar", "*sa/zzz", "*"},
		},
		{
			name:              "Two trust domain aliases, two principals",
			trustDomainBundle: NewTrustDomainBundle("td2", []string{"td1", "cluster.local"}),
			principals:        []string{"cluster.local/ns/foo/sa/bar", "td1/ns/yyy/sa/zzz"},
			expect: []string{"td2/ns/foo/sa/bar", "td1/ns/foo/sa/bar", "cluster.local/ns/foo/sa/bar",
				"td2/ns/yyy/sa/zzz", "td1/ns/yyy/sa/zzz", "cluster.local/ns/yyy/sa/zzz"},
		},
		{
			name:              "Two trust domain aliases with * prefix in trust domain",
			trustDomainBundle: NewTrustDomainBundle("td2", []string{"foo-td1", "cluster.local"}),
			principals:        []string{"*-td1/ns/foo/sa/bar"},
			expect:            []string{"td2/ns/foo/sa/bar", "*-td1/ns/foo/sa/bar", "cluster.local/ns/foo/sa/bar"},
		},
		{
			name:              "Principals not match alias",
			trustDomainBundle: NewTrustDomainBundle("td1", []string{"td2"}),
			principals:        []string{"some-td/ns/foo/sa/bar"},
			expect:            []string{"some-td/ns/foo/sa/bar"},
		},
		{
			name:              "Principals match one alias",
			trustDomainBundle: NewTrustDomainBundle("td1", []string{"td2", "some-td"}),
			principals:        []string{"some-td/ns/foo/sa/bar"},
			expect:            []string{"td1/ns/foo/sa/bar", "td2/ns/foo/sa/bar", "some-td/ns/foo/sa/bar"},
		},
		{
			name:              "One principal match one alias",
			trustDomainBundle: NewTrustDomainBundle("new-td", []string{"td2", "td3"}),
			principals:        []string{"td1/ns/some-ns/sa/some-sa", "td2/ns/foo/sa/bar"},
			expect: []string{"td1/ns/some-ns/sa/some-sa", "new-td/ns/foo/sa/bar",
				"td2/ns/foo/sa/bar", "td3/ns/foo/sa/bar"},
		},
	}

	for _, tc := range testCases {
		got := tc.trustDomainBundle.ReplaceTrustDomainAliases(tc.principals)
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
