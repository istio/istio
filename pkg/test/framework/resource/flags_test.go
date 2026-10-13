// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestValidate(t *testing.T) {
	tcs := []struct {
		name         string
		settings     *Settings
		expectErr    bool
		expectedRevs RevVerMap
	}{
		{
			name: "fail on deprecation and nocleanup",
			settings: &Settings{
				FailOnDeprecation: true,
				NoCleanup:         true,
			},
			expectErr: true,
		},
		{
			name: "fail on both revision and revisions flag",
			settings: &Settings{
				Revision:      "a",
				Compatibility: false,
				Revisions: RevVerMap{
					"b": "",
				},
			},
			expectErr: true,
		},
		{
			name: "fail when compatibility mode but no revisions",
			settings: &Settings{
				Compatibility: true,
			},
			expectErr: true,
		},
		{
			name: "revision flag converted to revvermap",
			settings: &Settings{
				Revision:      "a",
				Compatibility: false,
			},
			expectedRevs: RevVerMap{
				"a": "",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.settings)
			if tc.expectErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if tc.expectedRevs != nil {
				if diff := cmp.Diff(tc.expectedRevs, tc.settings.Revisions); diff != "" {
					t.Errorf("unexpected revisions, got: %v, want: %v, diff: %v",
						tc.settings.Revisions, tc.expectedRevs, diff)
				}
			}
		})
	}
}
