//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package tests

import (
	"github.com/google/go-cmp/cmp"
	"io/ioutil"
	"istio.io/istio/prow/asm/tester/pkg/resource"
	"strings"
	"testing"
)

func TestLabelMatches(t *testing.T) {
	tcs := []struct {
		name        string
		selectors   map[string]string
		matchLabels map[string]string
		expectError string
		expectMatch bool
	}{
		{
			name: "wrong value does not match",
			selectors: map[string]string{
				"control_plane": "unmanaged",
			},
			matchLabels: map[string]string{
				"control_plane": "managed",
			},
			expectMatch: false,
		},
		{
			name: "two matching values matches",
			selectors: map[string]string{
				"cluster_type": "gke-on-prem",
				"wip":          "hub",
			},
			matchLabels: map[string]string{
				"cluster_type": "gke-on-prem",
				"wip":          "hub",
			},
			expectMatch: true,
		},
		{
			name: "nonexistent key causes error",
			selectors: map[string]string{
				"fake_key": "fake_value",
			},
			matchLabels: map[string]string{},
			expectError: "unknown match key",
		},
		{
			name: "empty selectors matches",
			selectors: map[string]string{},
			matchLabels: map[string]string{
				"cluster_type": "gke-on-prem",
				"wip": "hub",
			},
			expectMatch: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			match, err := matches(tc.selectors, tc.matchLabels)
			if tc.expectError != "" {
				if err == nil {
					t.Fatalf("expected error %s, got none", tc.expectError)
				}
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Fatalf("expected error to contain %q, got %s", tc.expectError, err.Error())
				}
			} else if err != nil {
				t.Fatalf("got error %s, expected none", err.Error())
			}
			if tc.expectMatch != match {
				t.Errorf("expected match %t, got %t", tc.expectMatch, match)
			}
		})
	}
}

const sampleTestSkipConfig = `tests:
  - selectors:
    - control_plane: unmanaged
    targets:
    - names:
        - A
        - B
    - names:
        - C
  - selectors:
    - control_plane: unmanaged
      wip: hub
    targets:
    - names:
        - D
        - E
`

func TestSkipFlags(t *testing.T) {
	f, err := ioutil.TempFile("", "revision-config")
	if err != nil {
		t.Fatalf("failed creating skip test config file: %v", err)
	}
	_, err = f.WriteString(sampleTestSkipConfig)
	if err != nil {
		t.Fatalf("failed writing to temp skip test config file: %v", err)
	}
	skipTestConfig, err := parseSkipConfig(f.Name())
	if err != nil {
		t.Fatalf("failed to parse skip test config for %q: %v", f.Name(), err)
	}
	skipFlags, err := testSkipFlags(skipTestConfig.Tests, "", skipLabels(&resource.Settings{
		ControlPlane: "UNMANAGED",
		WIP:          "HUB",
	}))
	desired := []string{
		"--istio.test.skip=\"A\"",
		"--istio.test.skip=\"B\"",
		"--istio.test.skip=\"C\"",
		"--istio.test.skip=\"D\"",
		"--istio.test.skip=\"E\"",
	}
	if diff := cmp.Diff(skipFlags, desired); diff != "" {
		t.Errorf("got(+) different than want(-) %s", diff)
	}
}
