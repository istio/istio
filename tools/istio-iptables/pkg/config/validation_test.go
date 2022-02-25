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
	"testing"

	"istio.io/istio/pkg/test/util/assert"
	testdata "istio.io/istio/tools/istio-iptables/pkg/testing/data"
)

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
			include: testdata.NOwnerGroups(63), // just below the limit
		},
		{
			name:    "capture 64 groups",
			include: testdata.NOwnerGroups(64), // limit
		},
		{
			name:    "capture all but 64 groups",
			exclude: testdata.NOwnerGroups(64),
		},
		{
			name:    "capture all but 65 groups",
			exclude: testdata.NOwnerGroups(65), // we don't have to put a limit on the number of groups to exclude
		},
		{
			name:    "capture all but 1000 groups",
			exclude: testdata.NOwnerGroups(1000), // we don't have to put a limit on the number of groups to exclude
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
			include: testdata.NOwnerGroups(65), // just above the limit
		},
		{
			name:    "capture 100 groups",
			include: testdata.NOwnerGroups(100), // above the limit
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOwnerGroups(tc.include, tc.exclude)
			assert.Error(t, err)
		})
	}
}
