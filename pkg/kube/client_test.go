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

package kube

import (
	"context"
	"reflect"
	"testing"

	version2 "istio.io/pkg/version"
)

const istioNamespace = "istio-system"

func TestMockClient_GetIstioVersions(t *testing.T) {
	tests := []struct {
		version  string
		expected version2.BuildInfo
	}{
		{
			version: "1.12.0-016bc46f4a5e0ef3fa135b3c5380ab7765467c1a-dirty-Modified",
			expected: version2.BuildInfo{
				Version:       "1.12.0",
				GitRevision:   "016bc46f4a5e0ef3fa135b3c5380ab7765467c1a-dirty",
				GolangVersion: "",
				BuildStatus:   "Modified",
				GitTag:        "1.12.0",
			},
		},
		{
			version: "1.12.0-016bc46f4a5e0ef3fa135b3c5380ab7765467c1a-Clean",
			expected: version2.BuildInfo{
				Version:       "1.12.0",
				GitRevision:   "016bc46f4a5e0ef3fa135b3c5380ab7765467c1a",
				GolangVersion: "",
				BuildStatus:   "Clean",
				GitTag:        "1.12.0",
			},
		},
		{
			version: "1.12.0",
			expected: version2.BuildInfo{
				Version: "1.12.0",
			},
		},
	}
	for _, test := range tests {
		mc := MockClient{IstiodVersion: test.version}
		version, err := mc.GetIstioVersions(context.TODO(), istioNamespace)
		if err != nil {
			t.Fatal(err)
		}
		if version == nil {
			t.Fatal("no version obtained")
		}
		for _, info := range *version {
			if !reflect.DeepEqual(info.Info, test.expected) {
				t.Fatal("the version result is not the same as the expected one")
			}
		}
	}
}
