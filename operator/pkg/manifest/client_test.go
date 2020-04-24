// Copyright 2020 Istio Authors
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

package manifest

import "testing"

func Test_parseTag(t *testing.T) {
	cases := []struct {
		name        string
		image       string
		expectedTag string
	}{
		{
			name:        "correct tag in images",
			image:       "gcr.io/istio-testing/mynewproxy:1.0",
			expectedTag: "1.0",
		},
		{name: "no tag in images",
			image:       "gcr.io/istio-testing/mynewproxy:",
			expectedTag: "",
		},
		{name: "incorrect tag in images",
			image:       "gcr.io/istio-testing/mynewproxy",
			expectedTag: "",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTag(tt.image)
			if got != tt.expectedTag {
				t.Fatalf("got %v, want %v, err %v", got, tt.expectedTag, err)
			}
		})
	}
}
