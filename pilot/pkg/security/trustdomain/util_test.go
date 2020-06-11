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

package trustdomain

import (
	"testing"
)

func TestStringMatch(t *testing.T) {
	tests := []struct {
		desc   string
		a      string
		list   []string
		expect bool
	}{
		{
			desc:   "wildcard",
			a:      "*",
			list:   []string{"match-me"},
			expect: true,
		},
		{
			desc:   "suffix=match",
			a:      "yo*",
			list:   []string{"yo", "yolo", "yo-yo"},
			expect: true,
		},
		{
			desc:   "no-sub",
			a:      "foo",
			list:   []string{"bar"},
			expect: false,
		},
		{
			desc:   "prefix-match",
			a:      "*yo",
			list:   []string{"yoyo", "goyo"},
			expect: true,
		},
		{
			desc:   "empty",
			a:      "",
			list:   []string{"yo", "tawg"},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := stringMatch(tt.a, tt.list); got != tt.expect {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}
