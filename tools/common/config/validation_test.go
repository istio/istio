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

package config

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func NOwnerGroups(n int) string {
	var values []string
	for i := 0; i < n; i++ {
		values = append(values, strconv.Itoa(i))
	}
	return strings.Join(values, ",")
}

func TestValidateOwnerGroups_Valid(t *testing.T) {
	cases := []struct {
		name    string
		include string
		exclude string
	}{
		{
			name:    "capture all groups",
			include: "*",
		},
		{
			name:    "capture 63 groups",
			include: NOwnerGroups(63), // just below the limit
		},
		{
			name:    "capture 64 groups",
			include: NOwnerGroups(64), // limit
		},
		{
			name:    "capture all but 64 groups",
			exclude: NOwnerGroups(64),
		},
		{
			name:    "capture all but 65 groups",
			exclude: NOwnerGroups(65), // we don't have to put a limit on the number of groups to exclude
		},
		{
			name:    "capture all but 1000 groups",
			exclude: NOwnerGroups(1000), // we don't have to put a limit on the number of groups to exclude
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOwnerGroups(tc.include, tc.exclude)
			assert.NoError(t, err)
		})
	}
}

func TestValidateOwnerGroups_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		include string
		exclude string
	}{
		{
			name:    "capture 65 groups",
			include: NOwnerGroups(65), // just above the limit
		},
		{
			name:    "capture 100 groups",
			include: NOwnerGroups(100), // above the limit
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOwnerGroups(tc.include, tc.exclude)
			assert.Error(t, err)
		})
	}
}

func TestValidateIPv4LoopbackCidr_Valid(t *testing.T) {
	cases := []struct {
		name string
		cidr string
	}{
		{
			name: "valid IPv4 loopback CIDR",
			cidr: "127.0.0.1/32",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIPv4LoopbackCidr(tc.cidr)
			assert.NoError(t, err)
		})
	}
}

func TestValidateIPv4LoopbackCidr_Invalid(t *testing.T) {
	cases := []struct {
		name string
		cidr string
	}{
		{
			name: "invalid IPv4 loopback CIDR",
			cidr: "192.168.1.1/24",
		},
		{
			name: "invalid CIDR with mask below range",
			cidr: "10.0.0.1/7",
		},
		{
			name: "invalid CIDR with mask above range",
			cidr: "172.16.0.1/40",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIPv4LoopbackCidr(tc.cidr)
			assert.Error(t, err)
		})
	}
}

func TestValidateIptablesVersion(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected error
	}{
		{
			name:  "accepted_iptables_version",
			input: "legacy",
		},
		{
			name:  "empty_iptables_version",
			input: "",
		},
		{
			name:     "rejected_iptables_version",
			input:    "random",
			expected: fmt.Errorf("iptables version random not supported"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ValidateIptablesVersion(tc.input))
		})
	}
}
