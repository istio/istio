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

package util

import (
	"testing"
)

func TestConsolidateLog(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want string
	}{
		{
			desc: "empty",
			in:   "",
			want: "",
		},
		{
			desc: "2 errors once",
			in:   "err1\nerr2\n",
			want: "err1 (repeated 1 times)\nerr2 (repeated 1 times)\n",
		},
		{
			desc: "3 errors multiple times",
			in:   "err1\nerr2\nerr3\nerr1\nerr2\nerr3\nerr3\nerr3\n",
			want: "err1 (repeated 2 times)\nerr2 (repeated 2 times)\nerr3 (repeated 4 times)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := ConsolidateLog(tt.in), tt.want; !(got == want) {
				t.Errorf("%s: got:%s, want:%s", tt.desc, got, want)
			}
		})
	}
}
