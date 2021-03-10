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

package resource

import (
	"fmt"
	"testing"
)

func TestCompareIstioVersion(t *testing.T) {
	tcs := []struct {
		a, b   IstioVersion
		result int
	}{
		{
			IstioVersion("1.4"),
			IstioVersion("1.5"),
			-1,
		},
		{
			IstioVersion("1.9.0"),
			IstioVersion("1.10"),
			-1,
		},
		{
			IstioVersion("1.8.0"),
			IstioVersion("1.8.1"),
			-1,
		},
		{
			IstioVersion("1.9.1"),
			IstioVersion("1.9.1"),
			0,
		},
		{
			IstioVersion("1.12"),
			IstioVersion("1.3"),
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

func TestMinimumIstioVersion(t *testing.T) {
	tcs := []struct {
		name     string
		versions IstioVersions
		result   IstioVersion
	}{
		{
			"two versions",
			IstioVersions([]IstioVersion{
				"1.4", "1.5",
			}),
			IstioVersion("1.4"),
		},
		{
			"three versions",
			IstioVersions([]IstioVersion{
				"1.9", "1.13", "1.10",
			}),
			IstioVersion("1.9"),
		},
		{
			"single version",
			IstioVersions([]IstioVersion{
				"1.9",
			}),
			IstioVersion("1.9"),
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
