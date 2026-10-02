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

package krt

import "testing"

type selectorObject map[string]string

func (s selectorObject) GetLabelSelector() map[string]string {
	return s
}

func TestFilterSelectsNilLabels(t *testing.T) {
	cases := []struct {
		name     string
		selector map[string]string
		want     bool
	}{
		{name: "empty selector", selector: nil, want: true},
		{name: "non-empty selector", selector: map[string]string{"app": "a"}, want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			h := &dependency{filter: &filter{}}
			FilterSelects(nil)(h)
			if got := h.filter.Matches(selectorObject(tt.selector), false); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}
