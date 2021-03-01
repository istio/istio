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

package istio

import (
	"fmt"
	"testing"
)

func TestCompareVersion(t *testing.T) {
	tcs := []struct {
		a, b   Version
		result int
	}{
		{
			Version("1.4"),
			Version("1.5"),
			-1,
		},
		{
			Version("1.9.0"),
			Version("1.10"),
			-1,
		},
		{
			Version("1.8.0"),
			Version("1.8.1"),
			-1,
		},
		{
			Version("1.9.1"),
			Version("1.9.1"),
			0,
		},
		{
			Version("1.12"),
			Version("1.3"),
			1,
		},
	}

	for _, tc := range tcs {
		t.Run(fmt.Sprintf("compare version %s->%s", tc.a, tc.b), func(t *testing.T) {
			r := tc.a.Compare(tc.b)
			if r != tc.result {
				t.Errorf("expected %d, got %d", tc.result, r)
			}
		})
	}
}

func TestMinimumVersion(t *testing.T) {
	tcs := []struct {
		name     string
		versions Versions
		result   Version
	}{
		{
			"two versions",
			Versions([]Version{
				"1.4", "1.5",
			}),
			Version("1.4"),
		},
		{
			"three versions",
			Versions([]Version{
				"1.9", "1.13", "1.10",
			}),
			Version("1.9"),
		},
		{
			"single version",
			Versions([]Version{
				"1.9",
			}),
			Version("1.9"),
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			min := tc.versions.Minimum()
			if min != tc.result {
				t.Errorf("expected %v, got %v", tc.result, min)
			}
		})
	}
}
