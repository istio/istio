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

package main

import (
	"os"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/assert"
	testdata "istio.io/istio/tools/istio-iptables/pkg/testing/data"
	"istio.io/istio/tools/istio-iptables/pkg/testing/maintest"
)

func TestMain(m *testing.M) {
	maintest.TestMainOrRegularMain(m, main)
}

// TestConfig_Valid verifies whether command line flags and
// environment variables have desired effect.
func TestConfig_Valid(t *testing.T) {
	cases := []struct {
		name   string
		env    []string
		args   []string
		expect []string // confirm that user-defined configuration takes effect
	}{
		{
			name: "outbound-owner-groups-include",
			env:  []string{"ISTIO_OUTBOUND_OWNER_GROUPS=java,202"},
			args: []string{"--redirect-dns", "--dry-run"},
			expect: []string{
				"iptables -t nat -D OUTPUT -p udp --dport 53 -m owner ! --gid-owner java -m owner ! --gid-owner 202 -j RETURN",
				"ip6tables -t nat -D OUTPUT -p udp --dport 53 -m owner ! --gid-owner java -m owner ! --gid-owner 202 -j RETURN",
			},
		},
		{
			name: "outbound-owner-groups-exclude",
			env:  []string{"ISTIO_OUTBOUND_OWNER_GROUPS_EXCLUDE=888,ftp"},
			args: []string{"--redirect-dns", "--dry-run"},
			expect: []string{
				"iptables -t nat -D OUTPUT -p udp --dport 53 -m owner --gid-owner 888 -j RETURN",
				"iptables -t nat -D OUTPUT -p udp --dport 53 -m owner --gid-owner ftp -j RETURN",
				"ip6tables -t nat -D OUTPUT -p udp --dport 53 -m owner --gid-owner 888 -j RETURN",
				"ip6tables -t nat -D OUTPUT -p udp --dport 53 -m owner --gid-owner ftp -j RETURN",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binary := os.Args[0]                   // invoke test binary itself
			env := maintest.AskRegularMain(tc.env) // add env var that triggers execution of the regular `main()`
			args := tc.args

			stdout, err := shell.ExecuteArgs(env, false, binary, args...)
			assert.NoError(t, err)

			for _, expected := range tc.expect {
				present := containsLine(stdout, func(line string) bool {
					return strings.HasSuffix(line, expected)
				})
				if !present {
					t.Fatalf("expected: %s\ngot:\n%s", expected, stdout)
				}
			}
		})
	}
}

// TestConfig_Invalid verifies whether invalid values of command line flags
// and environment variables get rejected.
func TestConfig_Invalid(t *testing.T) {
	cases := []struct {
		name      string
		env       []string
		args      []string
		expectErr string // confirm that user-defined configuration takes effect
	}{
		{
			name:      "outbound-owner-groups-include",
			env:       []string{"ISTIO_OUTBOUND_OWNER_GROUPS=" + testdata.NOwnerGroups(65)},
			args:      []string{"--redirect-dns", "--dry-run"},
			expectErr: "number of owner groups whose outgoing traffic should be redirected to Envoy cannot exceed 64",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binary := os.Args[0]                   // invoke test binary itself
			env := maintest.AskRegularMain(tc.env) // add env var that triggers execution of the regular `main()`
			args := tc.args

			stdout, err := shell.ExecuteArgs(env, false, binary, args...)
			assert.Error(t, err)

			if !strings.Contains(stdout, tc.expectErr) {
				t.Fatalf("expected: %s\ngot:\n%s", tc.expectErr, stdout)
			}
		})
	}
}

func containsLine(text string, match func(string) bool) bool {
	for _, line := range strings.Split(text, "\n") {
		if match(line) {
			return true
		}
	}
	return false
}
