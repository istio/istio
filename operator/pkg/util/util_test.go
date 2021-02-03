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
	"errors"
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

func TestStringBoolMapToSlice(t *testing.T) {
	tests := []struct {
		desc string
		in   map[string]bool
		want []string
	}{
		{
			desc: "empty",
			in:   make(map[string]bool),
			want: make([]string, 0),
		},
		{
			desc: "",
			in: map[string]bool{
				"yo":           true,
				"yolo":         false,
				"test1":        true,
				"water bottle": false,
				"baseball hat": true,
			},
			want: []string{
				"yo",
				"test1",
				"baseball hat",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := StringBoolMapToSlice(tt.in), tt.want; !(sameStringSlice(got, want)) {
				t.Errorf("%s: got:%s, want: %s", tt.desc, got, want)
			}
		})
	}
}

// Helper function to check if values in 2 slices are the same,
// no correspondence to order
func sameStringSlice(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	diff := make(map[string]int, len(x))
	for _, _x := range x {
		diff[_x]++
	}
	for _, _y := range y {
		if _, ok := diff[_y]; !ok {
			return false
		}
		diff[_y]--
		if diff[_y] == 0 {
			delete(diff, _y)
		}
	}
	return len(diff) == 0
}

func TestRenderTemplate(t *testing.T) {
	type tmplValue struct {
		Name  string
		Proxy string
	}
	tests := []struct {
		desc     string
		template string
		in       tmplValue
		want     string
		err      error
	}{
		{
			desc:     "valid-template",
			template: "{{.Name}} uses {{.Proxy}} as sidecar",
			in: tmplValue{
				Name:  "istio",
				Proxy: "envoy",
			},
			want: "istio uses envoy as sidecar",
			err:  nil,
		},
		{
			desc:     "empty-template",
			template: "",
			in: tmplValue{
				Name:  "istio",
				Proxy: "envoy",
			},
			want: "",
			err:  nil,
		},
		{
			desc:     "template with no template strings",
			template: "this template is without handlebar expressions",
			in: tmplValue{
				Name:  "istio",
				Proxy: "envoy",
			},
			want: "this template is without handlebar expressions",
			err:  nil,
		},
		{
			desc:     "template with missing variable",
			template: "{{ .Name }} has replaced its control plane with {{ .Istiod }} component",
			in: tmplValue{
				Name:  "istio",
				Proxy: "envoy",
			},
			want: "",
			err:  errors.New(""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := RenderTemplate(tt.template, tt.in)
			if got != tt.want {
				t.Errorf("%s: got :%v, wanted output: %v", tt.desc, got, tt.want)
			}

			if (err == nil && tt.err != nil) || (err != nil && tt.err == nil) {
				t.Errorf("%s: got error :%v, wanted error: %v", tt.desc, err, tt.err)
			}
		})
	}
}
