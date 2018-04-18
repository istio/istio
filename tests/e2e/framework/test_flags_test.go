// Copyright 2017,2018 Istio Authors
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

package framework

import (
	"reflect"
	"testing"
)

func TestConfigVersions(t *testing.T) {
	cases := []struct {
		name      string
		testFlags TestFlags
		expected  []string
	}{
		{
			name:      "v1alpha1, v1alpha3",
			testFlags: TestFlags{V1alpha1: true, V1alpha3: true},
			expected:  []string{"v1alpha1", "v1alpha3"},
		},
		{
			name:      "v1alpha1",
			testFlags: TestFlags{V1alpha1: true, V1alpha3: false},
			expected:  []string{"v1alpha1"},
		},
		{
			name:      "v1alpha3",
			testFlags: TestFlags{V1alpha1: false, V1alpha3: true},
			expected:  []string{"v1alpha3"},
		},
		{
			name:      "empty",
			testFlags: TestFlags{V1alpha1: false, V1alpha3: false},
			expected:  []string{},
		},
	}

	for _, c := range cases {
		output := c.testFlags.ConfigVersions()
		if !reflect.DeepEqual(c.expected, output) {
			t.Errorf("%v failed: got %v but expected %v", c.name, output, c.expected)
		}
	}
}
