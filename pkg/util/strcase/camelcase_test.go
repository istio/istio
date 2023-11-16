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

package strcase

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestCamelCase(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"foo":           "Foo",
		"foobar":        "Foobar",
		"fooBar":        "FooBar",
		"foo_bar":       "FooBar",
		"foo-bar":       "FooBar",
		"foo_Bar":       "FooBar",
		"foo9bar":       "Foo9Bar",
		"HTTP-API-Spec": "HTTPAPISpec",
		"http-api-spec": "HttpApiSpec",
		"_foo":          "XFoo",
		"-foo":          "XFoo",
		"_Foo":          "XFoo",
		"-Foo":          "XFoo",
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			g := NewWithT(t)

			a := CamelCase(k)
			g.Expect(a).To(Equal(v))
		})
	}
}

func TestCamelCaseToKebabCase(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"Foo":                "foo",
		"FooBar":             "foo-bar",
		"foo9bar":            "foo9bar",
		"HTTPAPISpec":        "http-api-spec",
		"HTTPAPISpecBinding": "http-api-spec-binding",
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			g := NewWithT(t)

			a := CamelCaseToKebabCase(k)
			g.Expect(a).To(Equal(v))
		})
	}
}

func TestCamelCaseWithSeparator(t *testing.T) {
	type args struct {
		n   string
		sep string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "foo_bar",
			args: args{
				n:   "foo_bar",
				sep: "_",
			},
			want: "FooBar",
		},
		{
			name: "foo-bar",
			args: args{
				n:   "foo-bar",
				sep: "-",
			},
			want: "FooBar",
		},
		{
			name: "foo9bar",
			args: args{
				n:   "foo9bar",
				sep: "9",
			},
			want: "FooBar",
		},
		{
			name: "foo/bar",
			args: args{
				n:   "foo/bar",
				sep: "/",
			},
			want: "FooBar",
		},
		{
			name: "foobar",
			args: args{
				n:   "foobar",
				sep: "",
			},
			want: "FOOBAR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CamelCaseWithSeparator(tt.args.n, tt.args.sep); got != tt.want {
				t.Errorf("CamelCaseWithSeparator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isWordSeparator(t *testing.T) {
	tests := []struct {
		name string
		c    byte
		want bool
	}{
		{
			name: "_foo",
			c:    '_',
			want: true,
		},
		{
			name: "-foo",
			c:    '-',
			want: true,
		},
		{
			name: "foo",
			c:    'f',
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWordSeparator(tt.c); got != tt.want {
				t.Errorf("isWordSeparator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isASCIILower(t *testing.T) {
	tests := []struct {
		name string
		c    byte
		want bool
	}{
		{
			name: "A",
			c:    'A',
			want: false,
		},
		{
			name: "b",
			c:    'b',
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isASCIILower(tt.c); got != tt.want {
				t.Errorf("isASCIILower() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isASCIIDigit(t *testing.T) {
	tests := []struct {
		name string
		c    byte
		want bool
	}{
		{
			name: "A",
			c:    'A',
			want: false,
		},
		{
			name: "1",
			c:    '1',
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isASCIIDigit(tt.c); got != tt.want {
				t.Errorf("isASCIIDigit() = %v, want %v", got, tt.want)
			}
		})
	}
}
