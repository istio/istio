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

package features

import (
	"os"
	"testing"

	"istio.io/pkg/env"
)

func TestUnsafeFeaturesEnabled(t *testing.T) {
	cases := []struct {
		name     string
		allow    string
		vars     env.BoolVar
		expected bool
	}{
		{
			"allowed with no unsafe vars",
			"true",
			env.BoolVar{Var: env.Var{Name: "test", DefaultValue: "true"}},
			true,
		},
		{
			"allowed with unsafe vars",
			"true",
			env.BoolVar{Var: env.Var{Name: unsafePrefix + "test", DefaultValue: "true"}},
			true,
		},
		{
			"not allowed with no unsafe vars",
			"false",
			env.BoolVar{Var: env.Var{Name: "test", DefaultValue: "true"}},
			false,
		},
		{
			"not allowed with unsafe vars",
			"false",
			env.BoolVar{Var: env.Var{Name: unsafePrefix + "test", DefaultValue: "true"}},
			false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(unsafeFeatures, tc.allow)
			defer os.Unsetenv(unsafeFeatures)

			if enabled := UnsafeFeaturesEnabled(); enabled != tc.expected {
				t.Fatalf("expected : %v got %v", tc.expected, enabled)
			}
		})
	}
}

func TestAdminDebug(t *testing.T) {
	cases := []struct {
		name     string
		allow    string
		value    string
		expected bool
	}{
		{
			"allowed with default value",
			"true",
			"false",
			true,
		},
		{
			"allowed with set value",
			"true",
			"true",
			true,
		},
		{
			"not allowed with default",
			"false",
			"false",
			false,
		},
		{
			"not allowed with set",
			"false",
			"true",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(unsafeFeatures, tc.allow)
			os.Setenv("UNSAFE_ENABLE_ADMIN_ENDPOINTS", "true")
			defer os.Unsetenv(unsafeFeatures)
			defer os.Unsetenv("UNSAFE_ENABLE_ADMIN_ENDPOINTS")

			if enabled := EnableUnsafeAdminEndpoints(); enabled != tc.expected {
				t.Fatalf("expected : %v got %v", tc.expected, enabled)
			}
		})
	}
}
