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

func TestSplitEscaped(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want []string
	}{
		{
			desc: "empty",
			in:   "",
			want: []string{},
		},
		{
			desc: "no match",
			in:   "foo",
			want: []string{"foo"},
		},
		{
			desc: "first",
			in:   ":foo",
			want: []string{"", "foo"},
		},
		{
			desc: "last",
			in:   "foo:",
			want: []string{"foo", ""},
		},
		{
			desc: "multiple",
			in:   "foo:bar:baz",
			want: []string{"foo", "bar", "baz"},
		},
		{
			desc: "multiple with escapes",
			in:   `foo\:bar:baz\:qux`,
			want: []string{`foo\:bar`, `baz\:qux`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := splitEscaped(tt.in, kvSeparatorRune), tt.want; !stringSlicesEqual(got, want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func TestIsNPathElement(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect bool
	}{
		{
			desc:   "empty",
			in:     "",
			expect: false,
		},
		{
			desc:   "negative",
			in:     "[-45]",
			expect: false,
		},
		{
			desc:   "negative-1",
			in:     "[-1]",
			expect: true,
		},
		{
			desc:   "valid",
			in:     "[0]",
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := IsNPathElement(tt.in); got != tt.expect {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, aa := range a {
		if aa != b[i] {
			return false
		}
	}
	return true
}

func TestPathFromString(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect Path
	}{
		{
			desc:   "no-path",
			in:     "",
			expect: Path{},
		},
		{
			desc:   "valid-path",
			in:     "a.b.c",
			expect: Path{"a", "b", "c"},
		},
		{
			desc:   "surround-periods",
			in:     ".a.",
			expect: Path{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := PathFromString(tt.in); !got.Equals(tt.expect) {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func TestToYAMLPath(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect Path
	}{
		{
			desc:   "all-uppercase",
			in:     "A.B.C.D",
			expect: Path{"a", "b", "c", "d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := ToYAMLPath(tt.in); !got.Equals(tt.expect) {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func TestIsKVPathElement(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect bool
	}{
		{
			desc:   "valid",
			in:     "[1:2]",
			expect: true,
		},
		{
			desc:   "invalid",
			in:     "[:2]",
			expect: false,
		},
		{
			desc:   "invalid-2",
			in:     "[1:]",
			expect: false,
		},
		{
			desc:   "empty",
			in:     "",
			expect: false,
		},
		{
			desc:   "no-brackets",
			in:     "1:2",
			expect: false,
		},
		{
			desc:   "one-bracket",
			in:     "[1:2",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := IsKVPathElement(tt.in); got != tt.expect {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func TestIsVPathElement(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect bool
	}{
		{
			desc:   "valid",
			in:     "[:1]",
			expect: true,
		},
		{
			desc:   "kv-path-elem",
			in:     "[1:2]",
			expect: false,
		},
		{
			desc:   "invalid",
			in:     "1:2",
			expect: false,
		},
		{
			desc:   "empty",
			in:     "",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := IsVPathElement(tt.in); got != tt.expect {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func TestPathKV(t *testing.T) {
	tests := []struct {
		desc    string
		in      string
		wantK   string
		wantV   string
		wantErr error
	}{
		{
			desc:    "valid",
			in:      "[1:2]",
			wantK:   "1",
			wantV:   "2",
			wantErr: nil,
		},
		{
			desc:    "invalid",
			in:      "[1:",
			wantErr: errors.New(""),
		},
		{
			desc:    "empty",
			in:      "",
			wantErr: errors.New(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if k, v, err := PathKV(tt.in); k != tt.wantK || v != tt.wantV || errNilCheck(err, tt.wantErr) {
				t.Errorf("%s: expect %v %v %v got %v %v %v", tt.desc, tt.wantK, tt.wantV, tt.wantErr, k, v, err)
			}
		})
	}
}

func TestPathV(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want string
		err  error
	}{
		{
			desc: "valid-kv",
			in:   "[1:2]",
			want: "1:2",
			err:  nil,
		},
		{
			desc: "valid-v",
			in:   "[:1]",
			want: "1",
			err:  nil,
		},
		{
			desc: "invalid",
			in:   "083fj",
			want: "",
			err:  errors.New(""),
		},
		{
			desc: "empty",
			in:   "",
			want: "",
			err:  errors.New(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, err := PathV(tt.in); got != tt.want || errNilCheck(err, tt.err) {
				t.Errorf("%s: expect %v %v got %v %v", tt.desc, tt.want, tt.err, got, err)
			}
		})
	}
}

func TestRemoveBrackets(t *testing.T) {
	tests := []struct {
		desc       string
		in         string
		expect     string
		expectStat bool
	}{
		{
			desc:       "has-brackets",
			in:         "[yo]",
			expect:     "yo",
			expectStat: true,
		},
		{
			desc:       "one-bracket",
			in:         "[yo",
			expect:     "",
			expectStat: false,
		},
		{
			desc:       "other-bracket",
			in:         "yo]",
			expect:     "",
			expectStat: false,
		},
		{
			desc:       "no-brackets",
			in:         "yo",
			expect:     "",
			expectStat: false,
		},
		{
			desc:       "empty",
			in:         "",
			expect:     "",
			expectStat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, stat := RemoveBrackets(tt.in); got != tt.expect || stat != tt.expectStat {
				t.Errorf("%s: expect %v %v got %v %v", tt.desc, tt.expect, tt.expectStat, got, stat)
			}
		})
	}
}

func errNilCheck(err1, err2 error) bool {
	return (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil)
}
