// Copyright 2019 Istio Authors
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

func TestParseValue(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want interface{}
	}{
		{
			desc: "empty",
			in:   "",
			want: "",
		},
		{
			desc: "true",
			in:   "true",
			want: true,
		},
		{
			desc: "false",
			in:   "false",
			want: false,
		},
		{
			desc: "numeric-one",
			in:   "1",
			want: 1,
		},
		{
			desc: "numeric-zero",
			in:   "0",
			want: 0,
		},
		{
			desc: "numeric-large",
			in:   "12345678",
			want: 12345678,
		},
		{
			desc: "numeric-negative",
			in:   "-12345678",
			want: -12345678,
		},
		{
			desc: "float",
			in:   "1.23456",
			want: 1.23456,
		},
		{
			desc: "float-zero",
			in:   "0.00",
			want: 0.00,
		},
		{
			desc: "float-negative",
			in:   "-6.54321",
			want: -6.54321,
		},
		{
			desc: "string",
			in:   "foobar",
			want: "foobar",
		},
		{
			desc: "string-number-prefix",
			in:   "123foobar",
			want: "123foobar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := ParseValue(tt.in), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}
